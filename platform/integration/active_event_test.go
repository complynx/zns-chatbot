package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func secondOrderEvent(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.order_events(id,deadline,menu,extras) SELECT 'second-event',deadline,menu,extras FROM core.order_events WHERE id='sandbox-festival'`,
	)
	require.NoError(t, err)
}

func TestActiveEventCallbacksReplayAndNewRequests(t *testing.T) {
	t.Parallel()
	f := setup(t)
	secondOrderEvent(t, f)
	handleVisible(t, f.b, message(1, 101, "/orders"))
	oldCreate := orderClick(t, f, 101, 2, "Новый заказ")
	f.b.OrderEventID = "second-event"
	handle(t, f.b, oldCreate)
	handle(t, f.b, oldCreate)
	old, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, old, 1)
	newer, err := f.b.API.Orders(t.Context(), "alice", "second-event")
	require.NoError(t, err)
	require.Empty(t, newer)
	handleVisible(t, f.b, message(3, 101, "/orders"))
	handle(t, f.b, orderClick(t, f, 101, 4, "Новый заказ"))
	newer, err = f.b.API.Orders(t.Context(), "alice", "second-event")
	require.NoError(t, err)
	require.Len(t, newer, 1)
	assert.NotEqual(t, old[0].ID, newer[0].ID)
	foreign := oldCreate
	foreign.ID = 5
	foreign.Callback.From.ID = 202
	foreign.Callback.Message.Chat.ID = 202
	handle(t, f.b, foreign)
	list, err := f.b.API.Orders(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestActiveEventPendingReceiptAndMiniAppKeepOrderEvent(t *testing.T) {
	t.Parallel()
	f := setup(t)
	secondOrderEvent(t, f)
	order := intakeOrder(t, f, "old-order")
	command := orders.Command{EventID: order.EventID, OrderID: order.ID, Version: order.Version, Name: "proof"}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO bot.proof_pending(owner,command,selected_update) VALUES('alice',$1,1)`,
		command,
	)
	require.NoError(t, err)
	f.b.OrderEventID = "second-event"
	gateway := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: "second-event", ResolveOrderEvent: f.b.OrderEventForOrder}).Handler()
	path := "/miniapp/api/orders/" + order.ID
	response := webRequest(t, gateway, http.MethodGet, path, 101, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "sandbox-festival")
	assert.Equal(t, http.StatusNotFound, webRequest(t, gateway, http.MethodGet, path, 202, nil).Code)
	response = webRequest(
		t,
		gateway,
		http.MethodPost,
		"/miniapp/api/quote?order_id="+order.ID,
		101,
		orderChoice("preparty"),
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", OrderID: order.ID},
	}
	handle(t, f.b, photo)
	require.Equal(t, order.ID, f.model.input.MediaContext.SelectedOrderID)
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, "proof", current.State)
	f.b.OrderEventID = "sandbox-festival"
	handle(t, f.b, photo)
	after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, current.Version, after.Version)
	var saved orders.Command
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT command FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&saved),
	)
	assert.Equal(t, order.EventID, saved.EventID)
}

func TestActiveEventCachedAgentCommandKeepsEvent(t *testing.T) {
	t.Parallel()
	f := setup(t)
	secondOrderEvent(t, f)
	f.model.plan = agent.Plan{View: agent.OrdersView, OrderAction: &agent.OrderProposal{Name: "create"}}
	request := message(1, 101, "create order")
	handle(t, f.b, request)
	f.b.OrderEventID = "second-event"
	handle(t, f.b, request)
	handle(t, f.b, message(2, 101, "create another order"))
	for _, event := range []string{"sandbox-festival", "second-event"} {
		list, err := f.b.API.Orders(t.Context(), "alice", event)
		require.NoError(t, err)
		assert.Len(t, list, 1)
	}
	var raw json.RawMessage
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=1 AND kind='order_event'`).
			Scan(&raw),
	)
	assert.JSONEq(t, `"sandbox-festival"`, string(raw))
}

func TestActiveEventOpaqueReceiptChoiceKeepsOriginalOrder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	secondOrderEvent(t, f)
	first := intakeOrder(t, f, "first")
	intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"},
	}
	handleVisible(t, f.b, photo)
	choice := intakeChoice(t, f, 101, first.ID)
	f.b.OrderEventID = "second-event"
	handle(t, f.b, choice)
	current, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	require.Equal(t, "proof", current.State)
	handle(t, f.b, choice)
	replay, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, current.Version, replay.Version)
}
