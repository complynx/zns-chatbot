package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestLargeOrderListsDoNotBlockOtherTelegramUsers(t *testing.T) {
	t.Parallel()
	f := setup(t)
	menu, err := orders.Menu()
	require.NoError(t, err)
	name := strings.Repeat("n", 1024)
	choice, err := orders.Canonicalize(
		orders.ChoiceInput{Customer: name, FirstName: name, LastName: name, Patronymic: name,
			Days: map[string]orders.DayInput{"friday": {Mealtimes: map[string]orders.MealInput{
				"dinner": {Dishes: []orders.Item{{Name: "caesar", Count: 2}}},
			}}}},
		menu,
		orders.Extras(),
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.orders(id,event_id,owner,version,choice,state,attempt,attempt_at,payment_admin,country,created_at)
		SELECT 'bulk-'||lpad(i::text,4,'0'),'sandbox-festival','alice',1,$1,'cash','attempt-'||i,clock_timestamp(),'bob','be',
		'2026-01-01'::timestamptz+i*interval '1 second' FROM generate_series(1,320) i`,
		choice,
	)
	require.NoError(t, err)
	list, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 320)
	encoded, err := json.Marshal(list)
	require.NoError(t, err)
	assert.Greater(t, len(encoded), 1<<20, "fixture must exceed the old response limit")
	assert.Equal(t, "bulk-0001", list[0].ID)
	assert.Equal(t, "bulk-0320", list[319].ID)
	inbox, err := f.b.API.PaymentInbox(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	assert.Len(t, inbox, len(list))
	_, err = f.b.API.PaymentInbox(t.Context(), "alice", "sandbox-festival")
	requireCode(t, err, "forbidden")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, f.b.Handle(ctx, message(100, 101, "/orders")))
	var visibleOrders int
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		`SELECT count(*) FROM bot.order_cards WHERE owner='alice' AND card_key LIKE 'bulk-%' AND visible`,
	).Scan(&visibleOrders))
	assert.Equal(t, 10, visibleOrders, "Telegram rendering must be bounded without truncating API access")
	require.NoError(t, f.b.Handle(ctx, message(101, 202, "/start")))
	var cards int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.messages WHERE owner='bob'`).Scan(&cards))
	assert.Equal(t, 1, cards, "another user's next update must be processed")
	modelServer := httptest.NewServer((&agent.ScriptedServer{}).Handler())
	t.Cleanup(modelServer.Close)
	f.b.Model = agent.Remote{URL: modelServer.URL}
	handle(t, f.b, message(102, 101, "select massage"))
	workflow, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "massage-1", workflow.SlotID, "large order collection cannot disable unrelated agent requests")
	handle(t, f.b, message(103, 101, "add preparty to my order"))
	unchanged, err := f.b.API.Order(t.Context(), "alice", "sandbox-festival", "bulk-0320")
	require.NoError(t, err)
	assert.EqualValues(t, 1, unchanged.Version, "partial context must not pick an arbitrary order")
	handle(t, f.b, message(104, 101, "add preparty to order bulk-0001"))
	updated, err := f.b.API.Order(t.Context(), "alice", "sandbox-festival", "bulk-0001")
	require.NoError(t, err)
	assert.EqualValues(t, 2, updated.Version)
	assert.Equal(t, choice.Total+orders.Money(3500), updated.Choice.Total)
	assert.Equal(t, choice.Days, updated.Choice.Days, "agent extras edit must preserve meals and packaging")
	assert.Equal(t, name, updated.Choice.FirstName, "context compaction must not erase full saved choices")
	_, err = f.b.API.Order(t.Context(), "bob", "sandbox-festival", updated.ID)
	requireCode(t, err, "order_not_found")
	handle(t, f.b, message(105, 101, "add preparty to order bulk-0320"))
	offPage, err := f.b.API.Order(t.Context(), "alice", "sandbox-festival", "bulk-0320")
	require.NoError(t, err)
	assert.EqualValues(t, 2, offPage.Version)
	assert.Contains(t, pagingCard(t, f, 101, "bulk-0320").Text, "версия 2")
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "32/32")
}

func TestOrdersHTTPContract(t *testing.T) {
	t.Parallel()
	db := database(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	server := httptest.NewServer(
		api.Handler(runtimeapp.NewServices(db, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	t.Cleanup(server.Close)
	client := bot.APIClient{Base: server.URL, Signer: signer, HTTP: server.Client()}
	event, err := client.OrderEvent(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.Equal(t, orders.Money(6500), event.Extras["shuttle"].Price)
	choice := orderChoice("shuttle")
	quote, err := client.QuoteOrder(t.Context(), "alice", event.ID, *choice)
	require.NoError(t, err)
	assert.Equal(t, orders.Money(6500), quote.Total)
	command := orders.Command{EventID: event.ID, Name: "create", Origin: "manual", Key: "create", Choice: choice}
	created, err := client.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, "alice", created.Owner)
	assert.Equal(t, quote, created.Choice)
	replay, err := client.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, created, replay)
	listed, err := client.Orders(t.Context(), "bob", event.ID)
	require.NoError(t, err)
	assert.Empty(t, listed)
	_, err = client.ExecuteOrder(t.Context(), "bob", orderCommand("delete", created))
	requireCode(t, err, "order_not_found")
	_, err = client.ExecuteOrder(t.Context(), "visitor", command)
	requireCode(t, err, "forbidden")
	_, err = client.QuoteOrder(t.Context(), "visitor", event.ID, *choice)
	requireCode(t, err, "forbidden")
}

func TestOrdersHTTPRejectsMalformedAndUnauthenticatedRequests(t *testing.T) {
	t.Parallel()
	db := database(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(runtimeapp.NewServices(db, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler))
	for _, body := range []string{
		`null`, `{"days":null}`, `{"extras":null}`, `{"days":{"fri":null}}`,
		`{"days":{"fri":{"mealtimes":null}}}`, `{"days":{"fri":{"mealtimes":{"dinner":null}}}}`,
		`{"days":{"fri":{"mealtimes":{"dinner":{"dishes":null}}}}}`, `{"owner":"bob"}`,
		`{"days":{"fri":{"hidden":true}}}`, `{} {}`, strings.Repeat(" ", 65537),
	} {
		t.Run(body[:min(len(body), 100)], func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(
				http.MethodPost,
				"/v1/order-events/sandbox-festival/quote",
				strings.NewReader(body),
			)
			request.Header.Set("Authorization", "Bearer "+signer.Token("alice"))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assert.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/order-events/sandbox-festival/orders", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	var problem core.ProblemError
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "unauthorized", problem.Code)
}
