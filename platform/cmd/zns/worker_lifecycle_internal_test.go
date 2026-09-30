package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const workerTestTimeout = time.Second

func awaitWorker(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(workerTestTimeout):
		t.Fatal(what)
	}
}

func TestFatalLatchRetainsFirstDatabaseFailureAfterOrdinaryCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	latch := newFatalLatch(cancel)
	cancel() // ordinary shutdown wins the context first
	latch.report(errors.Join(core.ErrDatabase, context.Canceled))
	latch.report(core.ErrDatabase)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	err := latch.result(nil)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
	require.NoError(t, latch.result(nil), "the first failure is retained once")
	require.ErrorIs(t, newFatalLatch(cancel).result(context.Canceled), context.Canceled)
}

func TestFatalLatchIgnoresNonDatabaseReports(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	latch := newFatalLatch(cancel)
	for _, err := range []error{nil, context.Canceled, context.DeadlineExceeded, errors.New("provider unavailable")} {
		latch.report(err)
	}
	require.NoError(t, ctx.Err(), "only a database failure cancels siblings")
	require.NoError(t, latch.result(nil))
}

// A database failure from one worker is recorded, cancels a sibling that is
// blocked in an active operation, and prevents later ticks of the failing
// worker. Both stops join before the owner reads the result.
func TestTickWorkerDatabaseFailureCancelsSiblingAndStopsLaterTicks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	latch := newFatalLatch(cancel)
	siblingActive, siblingCanceled := make(chan struct{}), make(chan struct{})
	var siblingTicks atomic.Int32
	stopSibling := startTicks(ctx, time.Millisecond, func(live context.Context) error {
		if siblingTicks.Add(1) == 1 {
			close(siblingActive)
			<-live.Done()
			close(siblingCanceled)
		}
		return nil
	}, latch.report)
	awaitWorker(t, siblingActive, "sibling tick did not start")
	var failingTicks atomic.Int32
	stopFailing := startTicks(ctx, time.Millisecond, func(context.Context) error {
		if failingTicks.Add(1) == 1 {
			return nil
		}
		return core.ErrDatabase
	}, latch.report)
	awaitWorker(t, siblingCanceled, "database failure did not cancel the sibling")
	stopFailing()
	stopSibling()
	ticks := failingTicks.Load()
	require.Equal(t, int32(2), ticks, "no tick starts after the fatal report")
	time.Sleep(10 * time.Millisecond)
	require.Equal(t, ticks, failingTicks.Load())
	require.ErrorIs(t, latch.result(nil), core.ErrDatabase)
}

// Ordinary shutdown starts first. The active tick then finishes bounded work;
// stop waits for it, and a database failure seen there survives to the owner.
func TestTickWorkerStopJoinsActiveTickAndRetainsCleanupFailure(t *testing.T) {
	t.Parallel()
	for name, fatal := range map[string]bool{"database": true, "cancellation": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			latch := newFatalLatch(cancel)
			active, release := make(chan struct{}), make(chan struct{})
			var once atomic.Bool
			stop := startTicks(ctx, time.Millisecond, func(live context.Context) error {
				if !once.CompareAndSwap(false, true) {
					return nil
				}
				close(active)
				<-live.Done()
				<-release
				if fatal {
					return errors.Join(core.ErrDatabase, live.Err())
				}
				return nil
			}, latch.report)
			awaitWorker(t, active, "tick did not start")
			cancel()
			stopped := make(chan struct{})
			go func() { stop(); close(stopped) }()
			select {
			case <-stopped:
				t.Fatal("stop returned before the active tick finished")
			case <-time.After(50 * time.Millisecond):
			}
			close(release)
			awaitWorker(t, stopped, "stop did not join the active tick")
			result := latch.result(nil)
			if fatal {
				require.ErrorIs(t, result, core.ErrDatabase)
			} else {
				require.NoError(t, result, "ordinary cancellation stays normal")
			}
		})
	}
}

// unreachableServices fails every connection with private driver details.
func unreachableServices(t *testing.T) appservices.Services {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("private-dial-canary")
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return appservices.NewServices(pool, appservices.Options{})
}

// Media cleanup is the first SQL step when no announcement bindings are
// configured; its outage must end the pass with the safe marker, unlogged.
func TestMaintenanceTickReturnsSafeDatabaseFailure(t *testing.T) {
	t.Parallel()
	services := unreachableServices(t)
	var logs bytes.Buffer
	err := runMaintenanceTick(t.Context(), services, slog.New(slog.NewJSONHandler(&logs, nil)), time.Hour)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
	assert.Empty(t, logs.String(), "a fatal pass is not logged as an optional warning")
	for _, private := range []string{"private-user", "private-db", "private-dial-canary"} {
		assert.NotContains(t, err.Error(), private)
	}
}

// Food reminder startup is a hard failure and its tick a fatal one; both must
// carry only the safe marker, never the driver's private connection details.
func TestFoodMaintenanceReturnsSafeDatabaseFailure(t *testing.T) {
	t.Parallel()
	services := unreachableServices(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	stop, err := startFoodMaintenance(t.Context(), services.LegacyFood, logger, func(reported error) {
		t.Errorf("startup failure must not be reported as a worker fatal: %v", reported)
	})
	assert.Nil(t, stop)
	require.EqualError(t, err, core.ErrDatabase.Error())
	var connect *pgconn.ConnectError
	require.NotErrorAs(t, err, &connect, "driver diagnostics must not survive")

	err = foodReminderTick(services.LegacyFood, logger)(t.Context())
	require.EqualError(t, err, core.ErrDatabase.Error())
	assert.Empty(t, logs.String(), "a fatal tick is not logged as an optional warning")
}

func TestAnnouncementRefreshWithoutBindingsIsNoop(t *testing.T) {
	t.Parallel()
	require.NoError(t, refreshAnnouncementBindings(t.Context(), unreachableServices(t), slog.New(slog.DiscardHandler)))
}
