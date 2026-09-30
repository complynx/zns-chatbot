package integration_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type pollFailureQueryKey struct{}

// Cancel only after PostgreSQL has returned a real inbox write error. This
// orders the SQL failure before shutdown without depending on timing sleeps.
type cancelAfterInboxFailure struct {
	cancel context.CancelFunc
	seen   atomic.Bool
}

func (*cancelAfterInboxFailure) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	return context.WithValue(ctx, pollFailureQueryKey{}, strings.Contains(data.SQL, "INSERT INTO bot.telegram_inbox"))
}

func (tr *cancelAfterInboxFailure) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if target, _ := ctx.Value(pollFailureQueryKey{}).(bool); target && data.Err != nil {
		tr.seen.Store(true)
		tr.cancel()
	}
}

func TestPollDatabaseFailureSurvivesConcurrentCancellation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "first"})
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION bot.reject_cancelled_poll() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'private poll SQL diagnostic'; END $$;
CREATE TRIGGER reject_cancelled_poll BEFORE INSERT ON bot.telegram_inbox
FOR EACH ROW EXECUTE FUNCTION bot.reject_cancelled_poll()`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	trace := &cancelAfterInboxFailure{cancel: cancel}
	poolConfig := f.db.Config()
	poolConfig.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f.b.DB = pool
	err = f.b.Run(ctx)
	require.True(t, trace.seen.Load(), "the failed inbox SQL must trigger cancellation")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "private poll SQL diagnostic")
	var received int64
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT value FROM bot.cursors WHERE name='telegram_received'`).Scan(&received))
	require.Zero(t, received, "failed receipt must not acknowledge the upstream update")
	require.Zero(t, f.model.calls)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_cancelled_poll ON bot.telegram_inbox`)
	require.NoError(t, err)
	f.b.DB = f.db
	completeInbox(t, f, 2)
	require.Equal(t, 1, f.model.calls)
}

func TestPollingStartupDatabaseFailureReleasesOwnershipAndRecovers(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "first"})
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION bot.reject_cursor_startup() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'private cursor startup SQL diagnostic'; END $$;
CREATE TRIGGER reject_cursor_startup BEFORE INSERT OR UPDATE ON bot.cursors
FOR EACH ROW EXECUTE FUNCTION bot.reject_cursor_startup()`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = f.b.Run(ctx)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "private cursor startup SQL diagnostic")
	require.NoError(t, ctx.Err())
	require.Zero(t, f.model.calls, "startup SQL failure must prevent business handling")
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_cursor_startup ON bot.cursors`)
	require.NoError(t, err)
	completeInbox(t, f, 2)
	require.Equal(t, 1, f.model.calls, "fresh Run must reacquire ownership and process the retained upstream update")
}
