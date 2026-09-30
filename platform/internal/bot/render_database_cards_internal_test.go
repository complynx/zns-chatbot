package bot

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestRenderDatabaseCardOrigins(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	b := &Bot{DB: db}
	_, err = b.localizeWorkflowNotice(t.Context(), "alice", 1, "en", "fallback")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = b.orderButton(t.Context(), "alice", "label", orders.Command{})
	require.ErrorIs(t, err, core.ErrDatabase)
	require.ErrorIs(t, b.saveOrderPage(t.Context(), "alice", ownOrdersScope, 0, 0), core.ErrDatabase)
	_, err = b.currentOrderPage(t.Context(), "alice", ownOrdersScope, 1)
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = b.orderPageButton(t.Context(), "alice", ownOrdersScope, "label", 1)
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = b.refundCursor(t.Context(), "alice")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.ErrorIs(t, b.bindPaymentSource(t.Context(), "alice", "order", nil), core.ErrDatabase)
	_, err = b.paymentSource(t.Context(), "alice", "order")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.ErrorIs(t, b.refreshPaymentInstructions(t.Context(), "alice", 1, orders.Order{}, nil), core.ErrDatabase)
}

func TestRenderDatabaseCardJSONAndMissingControls(t *testing.T) {
	t.Parallel()
	b := &Bot{DB: foodPendingDatabase(t)}
	page, err := b.currentOrderPage(t.Context(), "alice", ownOrdersScope, 1)
	require.NoError(t, err)
	require.Zero(t, page)
	cursor, err := b.refundCursor(t.Context(), "alice")
	require.NoError(t, err)
	require.EqualValues(t, -1, cursor)
	_, err = b.paymentSource(t.Context(), "alice", "order")
	require.ErrorIs(t, err, appclient.ErrReadStale)
	text, err := b.localizeWorkflowNotice(t.Context(), "alice", 1, "en", "fallback")
	require.NoError(t, err)
	require.Equal(t, "fallback", text)
	_, err = b.DB.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES
 ('alice',1,$1,'"wrong type"'),('alice',0,'payment_source:order','"wrong type"'),('alice',1,'result','"wrong type"')`,
		"refund_page:"+b.currentOrderEvent())
	require.NoError(t, err)
	var malformed *json.UnmarshalTypeError
	_, err = b.refundCursor(t.Context(), "alice")
	require.ErrorAs(t, err, &malformed)
	require.False(t, core.IsDatabaseFailure(err))
	_, err = b.paymentSource(t.Context(), "alice", "order")
	require.ErrorAs(t, err, &malformed)
	require.False(t, core.IsDatabaseFailure(err))
	_, err = b.localizeWorkflowNotice(t.Context(), "alice", 1, "en", "fallback")
	require.ErrorAs(t, err, &malformed)
	require.False(t, core.IsDatabaseFailure(err))
}
