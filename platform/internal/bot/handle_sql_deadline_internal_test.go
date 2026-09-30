package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func handleDeadlineCalls() map[string]func(context.Context, *Bot) error {
	return map[string]func(context.Context, *Bot) error{
		"record insert": func(ctx context.Context, b *Bot) error {
			return b.record(ctx, "alice", 1, "order_event", "event")
		},
		"page binding": func(ctx context.Context, b *Bot) error {
			_, err := b.orderCallbackEvent(
				ctx, incoming{owner: "alice", text: orderPagePrefix + "token"}, 1, "event",
			)
			return err
		},
		"order binding": func(ctx context.Context, b *Bot) error {
			_, err := b.orderCallbackEvent(
				ctx, incoming{owner: "alice", text: orderCallbackPrefix + "token"}, 1, "event",
			)
			return err
		},
		"card lookup": func(ctx context.Context, b *Bot) error {
			return b.deliverCard(ctx, "alice", telegram.Send{ChatID: 1, Text: "notice"})
		},
		"saved reply": func(ctx context.Context, b *Bot) error {
			_, _, err := b.recordedKnowledgeReply(ctx, "alice", 1)
			return err
		},
		"workflow notice": func(ctx context.Context, b *Bot) error {
			_, err := b.localizeWorkflowNotice(ctx, "alice", 1, "en", "fallback")
			return err
		},
		"reconcile query": func(ctx context.Context, b *Bot) error {
			return b.reconcileViews(ctx)
		},
	}
}

// Exercise pgconn's connection-error wrapping, not merely the classifier helper.
func handleDeadlinePool(t *testing.T, failure error) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	dials := new(atomic.Int32)
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("private driver deadline: %w", failure)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, dials
}

func TestHandleSQLLocalContextFailureRetainsOrigin(t *testing.T) {
	t.Parallel()
	for name, call := range handleDeadlineCalls() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
				t.Run(failure.Error(), func(t *testing.T) {
					t.Parallel()
					db, dials := handleDeadlinePool(t, failure)
					err := call(t.Context(), &Bot{DB: db})
					require.Positive(t, dials.Load())
					require.NoError(t, t.Context().Err(), "the request is alive despite a driver-local failure")
					requireSafeDatabaseFailure(t, err)
					outcome, _ := classifyInboxResult(t.Context(), err, 0)
					require.Equal(t, inboxStop, outcome)
				})
			}
		})
	}
}

func TestHandleSQLActualParentCancellation(t *testing.T) {
	t.Parallel()
	for name, call := range handleDeadlineCalls() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, _ := handleDeadlinePool(t, context.Canceled)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			err := call(ctx, &Bot{DB: db})
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, core.IsDatabaseFailure(err))
		})
	}
}

func TestHandleSQLAbsenceAndJSONControls(t *testing.T) {
	t.Parallel()
	t.Run("saved reply absent", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil, r37NoRows(pgtype.JSONBOID))
		_, found, err := b.recordedKnowledgeReply(t.Context(), "alice", 1)
		require.NoError(t, err)
		require.False(t, found)
	})
	t.Run("callback binding absent", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil, r37NoRows(pgtype.TextOID))
		_, err := b.orderCallbackEvent(
			t.Context(), incoming{owner: "alice", text: orderPagePrefix + "missing"}, 1, "event",
		)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		require.False(t, core.IsDatabaseFailure(err))
	})
	t.Run("workflow absent", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil, r37NoRows(pgtype.TextOID, pgtype.JSONBOID))
		text, err := b.localizeWorkflowNotice(t.Context(), "alice", 1, "en", "fallback")
		require.NoError(t, err)
		require.Equal(t, "fallback", text)
	})
	t.Run("saved reply incompatible JSON", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil, r37Rows([]uint32{pgtype.JSONBOID}, []byte(`{}`)))
		_, found, err := b.recordedKnowledgeReply(t.Context(), "alice", 1)
		var invalid *json.UnmarshalTypeError
		require.ErrorAs(t, err, &invalid)
		require.True(t, found)
		require.False(t, core.IsDatabaseFailure(err))
	})
	t.Run("workflow incompatible JSON", func(t *testing.T) {
		t.Parallel()
		b, _ := sqlBot(t, nil,
			r37Rows([]uint32{pgtype.TextOID, pgtype.JSONBOID}, []byte("error"), []byte(`{}`)),
		)
		_, err := b.localizeWorkflowNotice(t.Context(), "alice", 1, "en", "fallback")
		var invalid *json.UnmarshalTypeError
		require.ErrorAs(t, err, &invalid)
		require.False(t, core.IsDatabaseFailure(err))
	})
}

func TestHandleProviderAndSerializationRemainNonSQL(t *testing.T) {
	t.Parallel()
	db, dials := handleDeadlinePool(t, context.DeadlineExceeded)
	b := &Bot{DB: db, API: mediaRenderAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		mediaRenderReply(w, http.StatusServiceUnavailable, `{"code":"provider_unavailable"}`)
	})}
	_, err := b.OrderEventForOrder(t.Context(), "alice", "order")
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	err = b.record(t.Context(), "alice", 1, "order_event", make(chan int))
	var unsupported *json.UnsupportedTypeError
	require.ErrorAs(t, err, &unsupported)
	require.False(t, core.IsDatabaseFailure(err))
	require.Zero(t, dials.Load(), "mixed provider and JSON errors must stop before SQL")
}
