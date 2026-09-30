package bot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestNotificationFollowupSQLFailureDoesNotComplete(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		call func(*Bot, context.Context) error
	}{
		{"orders", func(b *Bot, ctx context.Context) error {
			return b.deliverNotification(ctx, orders.Notification{ID: 1, Recipient: "alice", Current: true, FollowupPending: true})
		}},
		{"passes", func(b *Bot, ctx context.Context) error {
			return b.deliverPassNotification(ctx, passbooking.Notification{ID: 1, Recipient: "alice", Current: true, FollowupPending: true})
		}},
		{"pass view read", func(b *Bot, ctx context.Context) error {
			return b.refreshPassNotificationViews(ctx, passbooking.Notification{ID: 1, Recipient: "alice"})
		}},
		{"massage", func(b *Bot, ctx context.Context) error {
			return b.deliverMassageNotice(ctx, "alice", 1, massage.DeliveryNotice{Current: true,
				Notice: massage.Notice{ID: 1, FollowupPending: true}})
		}},
		{"food", func(b *Bot, ctx context.Context) error {
			return b.deliverFoodNotification(ctx, legacyfood.Notification{ID: 1, Owner: "alice", Current: true,
				FollowupPending: true, Payload: []byte(`{}`)})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var connects, completions atomic.Int32
			config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
			require.NoError(t, err)
			config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
				connects.Add(1)
				return io.EOF
			}
			db, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(db.Close)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "followup") || strings.Contains(r.URL.Path, "complete") {
					completions.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(server.Close)
			b := &Bot{DB: db, Host: appclient.Host{Base: server.URL,
				UserToken: func(context.Context, string) (string, error) { return "test", nil }}}
			err = test.call(b, t.Context())
			require.Equal(t, core.ErrDatabase, err)
			require.Positive(t, connects.Load(), "fault must reach a SQL connection boundary")
			require.Zero(t, completions.Load(), "SQL failure must leave followup pending")
		})
	}
}

func TestPassNotificationViewSQLFailureAfterReceipt(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('notice-sql',clock_timestamp()+interval '1 day')`,
	)
	require.NoError(t, err)
	var id int64
	require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO core.pass_notifications
 (event_id,owner,recipient,kind,generation,payload) VALUES('notice-sql','alice','alice','assigned','1','{}') RETURNING id`).Scan(&id))
	fault := &receiptDatabaseFault{prefix: "SELECT EXISTS(SELECT 1 FROM bot.pass_views"}
	config := db.Config()
	config.ConnConfig.Tracer = fault
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	var completions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "followup") {
			completions.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	b := &Bot{DB: broken, Host: appclient.Host{Base: server.URL,
		UserToken: func(context.Context, string) (string, error) { return "test", nil }}}
	err = b.deliverPassNotification(t.Context(), passbooking.Notification{
		ID: id, Recipient: "alice", Current: true, FollowupPending: true, MessageID: 42,
	})
	require.True(t, fault.fired)
	require.NoError(t, fault.closeError)
	require.Equal(t, core.ErrDatabase, err)
	require.Zero(t, completions.Load())
	var messageID int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT message_id FROM bot.pass_notification_deliveries WHERE notice_id=$1`, id).
			Scan(&messageID),
	)
	require.EqualValues(t, 42, messageID, "successful receipt must survive the later view-query failure")
}
