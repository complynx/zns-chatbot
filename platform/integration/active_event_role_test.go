package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
)

func TestActiveEventSplitBotRoleResolvesThroughCore(t *testing.T) {
	t.Parallel()
	f := setup(t)
	secondOrderEvent(t, f)
	order := intakeOrder(t, f, "role-order")
	_, err := f.db.Exec(
		t.Context(),
		`GRANT USAGE ON SCHEMA bot TO zns_bot; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA bot TO zns_bot; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA bot TO zns_bot`,
	)
	require.NoError(t, err)
	cfg := f.db.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, roleErr := conn.Exec(ctx, "SET ROLE zns_bot")
		return roleErr
	}
	restricted, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(restricted.Close)
	_, err = restricted.Exec(t.Context(), `SELECT event_id FROM core.orders LIMIT 1`)
	var denied *pgconn.PgError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "42501", denied.Code)
	f.b.DB = restricted
	f.b.OrderEventID = "second-event"
	event, err := f.b.OrderEventForOrder(t.Context(), "alice", order.ID)
	require.NoError(t, err)
	assert.Equal(t, order.EventID, event)
	_, err = f.b.OrderEventForOrder(t.Context(), "bob", order.ID)
	requireCode(t, err, "order_not_found")
	gateway := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: "second-event", ResolveOrderEvent: f.b.OrderEventForOrder}).Handler()
	path := "/miniapp/api/orders/" + order.ID
	response := webRequest(t, gateway, http.MethodGet, path, 101, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = webRequest(
		t,
		gateway,
		http.MethodPost,
		"/miniapp/api/quote?order_id="+order.ID,
		101,
		orderChoice("preparty"),
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	save := map[string]any{"version": order.Version, "key": "role-miniapp-save", "choice": orderChoice("shuttle")}
	response = webRequest(t, gateway, http.MethodPost, path, 101, save)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "sandbox-festival")
	assert.Equal(t, http.StatusNotFound, webRequest(t, gateway, http.MethodPost, path, 202, save).Code)
	_, err = f.db.Exec(t.Context(), `UPDATE core.orders SET state='deleted' WHERE id=$1`, order.ID)
	require.NoError(t, err)
	_, err = f.b.OrderEventForOrder(t.Context(), "alice", order.ID)
	requireCode(t, err, "order_not_found")
}
