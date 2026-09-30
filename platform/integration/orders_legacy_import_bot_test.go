package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func legacyBotFixture(t *testing.T) (*fixture, orders.Service, orders.Order) {
	t.Helper()
	f := setup(t)
	// Same fixture composition as setup (synthetic delivery identity bound to every
	// domain copy), plus the legacy source bot required by imported callbacks.
	services := notificationFixtureServices(f.db, appservices.Options{LegacyOrderBotID: 77})
	require.Equal(t, int64(77), services.LegacyOrders.LegacyBotID)
	server := httptest.NewServer(api.Handler(services, f.b.Host.Signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	f.b.API.Base = server.URL
	f.b.Host.Base = f.b.API.Base
	service := services.LegacyOrders
	order, err := service.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "legacy-create",
		Choice:  orderChoice("preparty"),
	})
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.legacy_order_import_references
	(source_key,bot_id,event_id,source_domain,source_record_sha256,target_id,source_record)
	VALUES($1,77,$2,'orders',$3,$4,$5)`, strings.Repeat("a", 64), order.EventID, strings.Repeat("b", 64), order.ID,
		`{"_id":{"$oid":"`+legacyOrderObjectID+`"},"user_id":101}`)
	require.NoError(t, err)
	return f, service, order
}

func TestLegacyOrderBotDeleteBindsBeforeEffectsAndReplays(t *testing.T) {
	t.Parallel()
	f, service, order := legacyBotFixture(t)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Imported message"})
	require.NoError(t, err)
	update := aliceCallback(7101, old.ID, "orders|del|"+legacyOrderObjectID)
	var failures [2]error
	var workers sync.WaitGroup
	for index := range failures {
		workers.Go(func() { failures[index] = f.b.Handle(t.Context(), update) })
	}
	workers.Wait()
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	f.b.OrderEventID = "changed-process-default"
	handle(t, f.b, update)
	var state string
	var version int64
	require.NoError(
		t,
		service.DB.QueryRow(t.Context(), `SELECT state,version FROM core.orders WHERE id=$1`, order.ID).
			Scan(&state, &version),
	)
	assert.Equal(t, "deleted", state)
	assert.Equal(t, order.Version+1, version)
	var binding struct {
		Binding orders.LegacyCallbackBinding `json:"binding"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=7101 AND kind='legacy_order_binding'`).
			Scan(&binding),
	)
	assert.Equal(t, order.Version, binding.Binding.Command.Version)
	assert.Equal(t, order.EventID, binding.Binding.EventID)
}

func TestLegacyOrderBotSavedVersionCannotFollowLaterCash(t *testing.T) {
	t.Parallel()
	f, service, order := legacyBotFixture(t)
	data := "orders|del|" + legacyOrderObjectID
	binding, err := service.ResolveLegacyCallback(t.Context(), "alice", orders.LegacyCallbackRequest{Data: data})
	require.NoError(t, err)
	binding.Command.Key = "tg-legacy-order-7201"
	digest := sha256.Sum256([]byte(data))
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',7201,'legacy_order_binding',$1)`,
		map[string]any{"digest": hex.EncodeToString(digest[:]), "binding": binding},
	)
	require.NoError(t, err)
	cash := orderCommand("cash", order)
	cash.PaymentAdmin = "bob"
	current, err := service.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Imported message"})
	require.NoError(t, err)
	handle(t, f.b, aliceCallback(7201, old.ID, data))
	var state string
	var version int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT state,version FROM core.orders WHERE id=$1`, order.ID).
			Scan(&state, &version),
	)
	assert.Equal(t, "cash", state)
	assert.Equal(t, current.Version, version)
	var code string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content->>'code' FROM bot.interactions WHERE owner='alice' AND update_id=7201 AND kind='order_error'`).
			Scan(&code),
	)
	assert.Equal(t, "stale_version", code)
}

func TestLegacyOrderBotCloseInBothLocales(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, _, _ := legacyBotFixture(t)
			handleOrderVisible(t, f, message(7300, 101, "/language "+language))
			old, err := f.b.TG.Send(
				t.Context(),
				telegram.Send{
					ChatID: 101,
					Text:   "Imported message",
					Markup: telegram.Markup{Rows: [][]telegram.Button{{{Text: "Close", Data: "orders|close"}}}},
				},
			)
			require.NoError(t, err)
			handleOrderVisible(t, f, aliceCallback(7301, old.ID, "orders|close"))
			for _, message := range chatMessages(t, f, 101) {
				if message.ID == old.ID {
					assert.Contains(
						t,
						message.Text,
						map[string]string{"en": "Orders closed.", "ru": "Заказы закрыты."}[language],
					)
					assert.Empty(t, message.Markup.Rows)
					return
				}
			}
			t.Fatal("closed message missing")
		})
	}
}

func TestLegacyOrderBotExportRequiresCurrentAdmin(t *testing.T) {
	t.Parallel()
	f, _, _ := legacyBotFixture(t)
	old, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 202, Text: "Imported export button"})
	require.NoError(t, err)
	update := aliceCallback(7401, old.ID, "orders|xlsx")
	update.Callback.From.ID, update.Callback.Message.Chat.ID = 202, 202
	handleOrderVisible(t, f, update)
	handleOrderVisible(t, f, update) // Replay after delivery must not export twice.
	var sent int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND kind='order_export'`).
			Scan(&sent),
	)
	assert.Equal(t, 1, sent)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.order_admins WHERE owner='bob'`)
	require.NoError(t, err)
	handleOrderVisible(t, f, update)
	for _, message := range chatMessages(t, f, 202) {
		if message.ID == old.ID {
			assert.Contains(t, message.Text, "forbidden")
		}
	}
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	old, err = f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Imported menu button"})
	require.NoError(t, err)
	before := chatMessages(t, f, 101)
	handleOrderVisible(t, f, aliceCallback(7402, old.ID, "orders|start"))
	after := chatMessages(t, f, 101)
	assert.Len(t, after, len(before), "denied navigation must not render order cards")
}
