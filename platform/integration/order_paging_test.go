package integration_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func seedPagingOrders(t *testing.T, f *fixture, cash bool) {
	t.Helper()
	menu, err := orders.Menu()
	require.NoError(t, err)
	choice, err := orders.Canonicalize(*orderChoice("shuttle"), menu, orders.Extras())
	require.NoError(t, err)
	state := "unpaid"
	if cash {
		state = "cash"
	}
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.orders(id,event_id,owner,version,choice,state,attempt,attempt_at,payment_admin,country,created_at)
 SELECT 'page-'||lpad(i::text,4,'0'),'sandbox-festival','alice',1,$1,$2,'attempt-'||i,clock_timestamp(),'bob','be',
 '2026-01-01'::timestamptz+i*interval '1 second' FROM generate_series(1,$3::int) i`,
		choice,
		state,
		23,
	)
	require.NoError(t, err)
}

func pagingCard(t *testing.T, f *fixture, user int64, key string) telegram.Message {
	t.Helper()
	owner := "alice"
	if user == 202 {
		owner = "bob"
	}
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT message_id FROM bot.order_cards WHERE owner=$1 AND card_key=$2`, owner, key).
			Scan(&id),
	)
	for _, message := range chatMessages(t, f, user) {
		if message.ID == id {
			return message
		}
	}
	t.Fatalf("card %s not found", key)
	return telegram.Message{}
}

func pageClick(t *testing.T, f *fixture, user, update int64, scope string, next bool) telegram.Update {
	t.Helper()
	card := pagingCard(t, f, user, "paging:"+scope)
	label := "Далее"
	if !next {
		label = "Назад"
	}
	for _, row := range card.Markup.Rows {
		for _, button := range row {
			if button.Text == label {
				return telegram.Update{
					ID: update,
					Callback: &telegram.Callback{
						ID:      strconv.FormatInt(update, 10),
						From:    telegram.User{ID: user},
						Message: card,
						Data:    button.Data,
					},
				}
			}
		}
	}
	t.Fatalf("no %s button in %s", label, card.Text)
	return telegram.Update{}
}

func TestOrderPagingNavigationRetiresOldCardsAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPagingOrders(t, f, false)
	handle(t, f.b, message(100, 101, "/orders"))
	first := pagingCard(t, f, 101, "paging:orders")
	assert.Contains(t, first.Text, "1/3")
	require.Len(t, first.Markup.Rows, 1)
	require.Len(t, first.Markup.Rows[0], 1)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner='alice' AND card_key LIKE 'page-%' AND visible`).
			Scan(&count),
	)
	assert.Equal(t, 10, count)
	next := pageClick(t, f, 101, 101, "orders", true)
	handle(t, f.b, next)
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "2/3")
	retired := pagingCard(t, f, 101, "page-0001")
	assert.Empty(t, retired.Markup.Rows)
	assert.Contains(t, retired.Text, "вне текущей страницы")
	assert.NotContains(t, retired.Text, "удалён")
	// New Bot instance uses the persisted view rather than an in-memory page counter.
	f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: f.model}
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "2/3")
	handle(t, f.b, pageClick(t, f, 101, 102, "orders", true))
	last := pagingCard(t, f, 101, "paging:orders")
	assert.Contains(t, last.Text, "3/3")
	require.Len(t, last.Markup.Rows[0], 1)
	assert.Equal(t, "Назад", last.Markup.Rows[0][0].Text)
	handle(t, f.b, next) // Delayed duplicate must not rewind the newer page.
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "3/3")
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner='alice' AND card_key LIKE 'page-%' AND visible`).
			Scan(&count),
	)
	assert.Equal(t, 3, count)
	for update := int64(103); update < 107; update += 2 {
		handle(t, f.b, pageClick(t, f, 101, update, "orders", false))
		handle(t, f.b, pageClick(t, f, 101, update+1, "orders", true))
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_page_buttons WHERE owner='alice'`).Scan(&count),
	)
	assert.Equal(t, 3, count, "stable navigation keys must not grow with each page click")
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_operations`).Scan(&count))
	assert.Zero(t, count, "navigation never executes a business command")
	var editsBefore, editsAfter int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT (data->>'Edits')::int FROM bot.fake_state WHERE id`).Scan(&editsBefore),
	)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT (data->>'Edits')::int FROM bot.fake_state WHERE id`).Scan(&editsAfter),
	)
	assert.Equal(t, editsBefore, editsAfter, "inactive history must not generate repeated Telegram writes")
	// Data changes clamp the saved page and retire controls for vanished items.
	_, err := f.db.Exec(t.Context(), `UPDATE core.orders SET state='deleted' WHERE id>'page-0005'`)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "1/1")
	assert.Empty(t, pagingCard(t, f, 101, "paging:orders").Markup.Rows)
	assert.Empty(t, pagingCard(t, f, 101, "page-0023").Markup.Rows)
	_, err = f.db.Exec(t.Context(), `UPDATE core.orders SET state='deleted'`)
	require.NoError(t, err)
	next.ID = 200
	handle(t, f.b, next)
	empty := pagingCard(t, f, 101, "paging:orders")
	assert.Contains(t, empty.Text, "1/1")
	assert.Contains(t, empty.Text, "всего 0")
	assert.Empty(t, empty.Markup.Rows)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner='alice' AND card_key LIKE 'page-%' AND visible`).
			Scan(&count),
	)
	assert.Zero(t, count)
}

func TestOrderPagingRejectsForgedAndForeignCallbacks(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPagingOrders(t, f, false)
	handle(t, f.b, message(100, 101, "/orders"))
	foreign := pageClick(t, f, 101, 101, "orders", true)
	foreign.Callback.From.ID = 202
	foreign.Callback.Message.Chat.ID = 202
	handle(t, f.b, foreign)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_pages WHERE owner='bob'`).Scan(&count),
	)
	assert.Zero(t, count)
	assert.Contains(t, chatMessages(t, f, 202)[0].Text, "недоступна")
	forged := pageClick(t, f, 101, 102, "orders", true)
	forged.Callback.Data = "o:page:" + strings.Repeat("0", 32)
	handle(t, f.b, forged)
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "1/3")
	for _, message := range chatMessages(t, f, 202) {
		assert.NotContains(t, message.Text, "page-0001")
	}
}

func TestOrderPagingAgentFocusesExplicitOffPageOrder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPagingOrders(t, f, false)
	handle(t, f.b, message(100, 101, "/orders"))
	f.model.plan = agent.Plan{
		View:        agent.OrdersView,
		OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: "page-0023", Extra: "preparty"},
	}
	handle(t, f.b, message(101, 101, "add preparty to order page-0023"))
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "3/3")
	card := pagingCard(t, f, 101, "page-0023")
	assert.Contains(t, card.Text, "версия 2")
	assert.Contains(t, card.Text, "Препати")
	assert.Empty(t, pagingCard(t, f, 101, "page-0001").Markup.Rows)
}

func TestPaymentInboxPagingIsIndependentAndRevocationAware(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPagingOrders(t, f, true)
	handle(t, f.b, message(100, 101, "/orders"))
	handle(t, f.b, message(101, 202, "/orders"))
	assert.Contains(t, pagingCard(t, f, 202, "paging:admin").Text, "1/3")
	next := pageClick(t, f, 202, 102, "admin", true)
	handle(t, f.b, next)
	assert.Contains(t, pagingCard(t, f, 202, "paging:admin").Text, "2/3")
	assert.Contains(t, pagingCard(t, f, 101, "paging:orders").Text, "1/3")
	assert.Empty(t, pagingCard(t, f, 202, "admin:page-0001").Markup.Rows)
	assert.NotEmpty(t, pagingCard(t, f, 202, "admin:page-0011").Markup.Rows)
	_, err := f.db.Exec(t.Context(), `DELETE FROM core.order_admins WHERE owner='bob'`)
	require.NoError(t, err)
	next.ID = 103
	handle(t, f.b, next)
	assert.Empty(t, pagingCard(t, f, 202, "admin:page-0011").Markup.Rows)
	assert.Empty(t, pagingCard(t, f, 202, "paging:admin").Markup.Rows)
}
