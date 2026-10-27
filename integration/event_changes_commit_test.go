package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestAgentEventChangeLostCommitReplyRecoversWithoutSecondMutation(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	body, _ := prepareEventBody(t, h, operator, "lost-event-commit", args)
	proxy := newCommitReplyProxy(t, h.store.Pool().Config().ConnString())
	store, err := db.Open(t.Context(), proxy.dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	h.store = store
	h.start(t)
	h.stop(t)
	proxy.armed.Store(true)
	code, raw := adminRequest(t, h, operator, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	select {
	case <-proxy.dropped:
	case <-time.After(time.Second):
		t.Fatal("the actual PostgreSQL COMMIT reply was not dropped")
	}
	receipt, err := h.store.EventChangeReceipt(t.Context(), scope, "lost-event-commit")
	require.NoError(t, err)
	require.EqualValues(t, 1, receipt.Version)
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "lost-event-commit"})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, *receipt, eventResult(t, raw))
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, *receipt, eventResult(t, raw))
	current, err := h.store.Event(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, args.ID)
	require.NoError(t, err)
	require.Equal(t, *receipt, current)
}
