package registrationingress

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type testClock struct {
	now    time.Time
	err    error
	cancel context.CancelFunc
}

func (c *testClock) Now(context.Context) (time.Time, error) {
	if c.cancel != nil {
		c.cancel()
	}
	return c.now, c.err
}

func TestClockFailureRetainsCancellationDuringObservation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	clock := &testClock{err: io.ErrUnexpectedEOF, cancel: cancel}
	_, _, err := Observe(ctx, clock)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestClockObservationIsLiveAndFailClosed(t *testing.T) {
	t.Parallel()
	clock := &testClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	ctx := WithClock(t.Context(), clock)
	first, _, err := observeContext(ctx)
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Minute)
	second, _, err := observeContext(ctx)
	require.NoError(t, err)
	require.Equal(t, time.Minute, second.Sub(first))
	clock.err = io.ErrUnexpectedEOF
	_, _, err = observeContext(ctx)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = observeContext(canceled)
	require.ErrorIs(t, err, context.Canceled)
	clock.err = nil
	clock.now = clock.now.Add(time.Nanosecond)
	_, _, err = observeContext(ctx)
	require.Error(t, err)
	defaultTime, configured, err := observeContext(WithClock(t.Context(), nil))
	require.NoError(t, err)
	require.Zero(t, defaultTime)
	require.False(t, configured)
}

type clockIngressTx struct {
	pgx.Tx

	args [][]any
}

func (tx *clockIngressTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.args = append(tx.args, args)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestTelegramClockPreservesDefaultQueryCount(t *testing.T) {
	t.Parallel()
	for _, controlled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "controlled"}[controlled], func(t *testing.T) {
			t.Parallel()
			tx := &clockIngressTx{}
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			ctx := t.Context()
			if controlled {
				ctx = WithClock(ctx, &testClock{now: now})
			}
			require.NoError(t, SaveTelegram(ctx, tx, Reference{BotID: 7, UpdateID: 1}, 101))
			require.Len(t, tx.args, 2, "same allocator lock and insert")
			received, ok := tx.args[1][6].(pgtype.Timestamptz)
			require.True(t, ok)
			require.Equal(t, controlled, received.Valid)
			if controlled {
				require.Equal(t, now, received.Time)
			}
		})
	}
}
