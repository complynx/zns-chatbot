package bot

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestBackgroundDatabaseWorkerReportsSafeFatalAndCancelsSibling(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fatal := make(chan error, 1)
	var passes atomic.Int32
	stop := startBotDelivery(ctx, func(context.Context) error {
		passes.Add(1)
		return &pgconn.PgError{Code: "08006", Message: "private driver detail"}
	}, func(err error) {
		fatal <- err
		cancel()
	})
	t.Cleanup(stop)
	select {
	case <-ctx.Done():
	case <-time.After(unlockTimeout):
		t.Fatal("database failure did not cancel sibling context")
	}
	result := finishBotDelivery(stop, fatal, nil)
	require.ErrorIs(t, result, core.ErrDatabase)
	require.NotContains(t, result.Error(), "private driver detail")
	require.EqualValues(t, 1, passes.Load())
	require.Empty(t, fatal)
}

func TestBackgroundDatabaseWorkerKeepsProviderFailureNonfatal(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded,
		&core.ProblemError{Status: 503, Code: "provider_unavailable"}} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			fatal := make(chan error, 1)
			var passes atomic.Int32
			stop := startBotDelivery(t.Context(), func(context.Context) error {
				if passes.Add(1) == 1 {
					return failure
				}
				return core.ErrDatabase
			}, func(err error) { fatal <- err })
			t.Cleanup(stop)
			select {
			case err := <-fatal:
				require.Equal(t, core.ErrDatabase, err)
			case <-time.After(unlockTimeout):
				t.Fatal("worker did not run its next pass")
			}
			stop()
			require.EqualValues(t, 2, passes.Load(), "provider or cancellation error must not terminate the worker")
		})
	}
}

func TestBackgroundDatabaseCompletionAfterCancellationSurvivesFinalization(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, finishing, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseWork := sync.OnceFunc(func() { close(release) })
	fatal := make(chan error, 1)
	stop := startBotDelivery(ctx, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		completion, done := deliveryCompletionContext(ctx)
		defer done()
		close(finishing)
		<-release
		if completion.Err() != nil {
			return completion.Err()
		}
		return errors.Join(core.ErrDatabase, ctx.Err())
	}, func(err error) {
		fatal <- err
		cancel()
	})
	t.Cleanup(stop)
	t.Cleanup(releaseWork)
	select {
	case <-entered:
	case <-time.After(unlockTimeout):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case <-finishing:
	case <-time.After(unlockTimeout):
		t.Fatal("bounded receipt completion did not start")
	}
	result := make(chan error, 1)
	go func() {
		joined := finishBotDelivery(stop, fatal, nil)
		result <- (&Bot{}).creditCutoverRunResult(ctx, joined)
	}()
	select {
	case <-result:
		t.Fatal("ownership cleanup could proceed before receipt completion joined")
	default:
	}
	releaseWork()
	select {
	case err := <-result:
		require.ErrorIs(t, err, core.ErrDatabase)
	case <-time.After(unlockTimeout):
		t.Fatal("completion did not join")
	}
	require.NoError(t, (&Bot{}).creditCutoverRunResult(ctx, context.Canceled))
}

func TestBackgroundDatabasePreparationPreservesFatalAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b := &Bot{}
	err := b.botPreparationFailure(ctx, botdelivery.Intent{}, errors.Join(core.ErrDatabase, context.Canceled))
	require.Equal(t, core.ErrDatabase, err)
	err = b.botPreparationFailure(ctx, botdelivery.Intent{}, io.EOF)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestBackgroundDatabaseDirectSQLAndContinuationSchedule(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	b := &Bot{DB: db, Delivery: delivery.Settings{
		BotID: 1, BotInterval: time.Millisecond, ChatInterval: time.Millisecond, Fallback: time.Second,
	}, Host: appclient.Host{UserToken: func(context.Context, string) (string, error) { return "", io.EOF }}}
	_, err = b.deliveryHeads(t.Context())
	require.ErrorIs(t, err, core.ErrDatabase)
	require.ErrorIs(t, b.ContinueBotIntentReceipts(t.Context()), core.ErrDatabase)
	require.ErrorIs(t, b.validateCreditCutover(t.Context()), core.ErrDatabase)
	err = b.continueBotIntent(t.Context(), botdelivery.Intent{
		Owner: "owner", Reference: botdelivery.Reference{Kind: botdelivery.IdentityIntent},
	})
	require.ErrorIs(t, err, io.EOF, "provider failure is preserved")
	require.ErrorIs(t, err, core.ErrDatabase, "SQL retry scheduling failure retains positive provenance")
}

func TestBackgroundDatabaseContinuationFailureDoesNotScheduleRetry(t *testing.T) {
	t.Parallel()
	b := &Bot{Delivery: botIntentTestSettings(), Host: appclient.Host{
		UserToken: func(context.Context, string) (string, error) {
			return "", core.ErrDatabase
		},
	}}
	// No database is needed: a known SQL failure must stop before retry scheduling.
	err := b.continueBotIntent(t.Context(), botdelivery.Intent{
		Owner: "owner", Reference: botdelivery.Reference{Kind: botdelivery.IdentityIntent},
	})
	require.Equal(t, core.ErrDatabase, err)
}

func TestBackgroundDatabaseContinuationCancellationSkipsRetrySchedule(t *testing.T) {
	t.Parallel()
	// An empty parent mode keeps the caller context live.
	const (
		parentCancel   = "cancel"
		parentDeadline = "deadline"
	)
	for _, test := range []struct {
		name     string
		failure  error
		parent   string
		want     error
		schedule bool
	}{
		{name: "cancelled after provider failure", failure: io.EOF, parent: parentCancel, want: context.Canceled},
		{name: "cancelled after SQL failure", failure: core.ErrDatabase, parent: parentCancel},
		{name: "parent deadline after provider failure", failure: io.EOF, parent: parentDeadline,
			want: context.DeadlineExceeded},
		{name: "parent deadline after SQL failure joined with deadline",
			failure: errors.Join(core.ErrDatabase, context.DeadlineExceeded), parent: parentDeadline},
		{name: "returned cancellation with live parent", failure: context.Canceled, want: context.Canceled},
		{name: "returned deadline with live parent", failure: context.DeadlineExceeded,
			want: context.DeadlineExceeded},
		{name: "returned SQL failure joined with cancellation with live parent",
			failure: errors.Join(core.ErrDatabase, context.Canceled)},
		{name: "live provider failure", failure: io.EOF, schedule: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
			require.NoError(t, err)
			var connects atomic.Int32
			config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
				connects.Add(1)
				return io.EOF
			}
			db, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(db.Close)
			var ctx context.Context
			var cancel context.CancelFunc
			if test.parent == parentDeadline {
				ctx, cancel = context.WithTimeout(t.Context(), time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			defer cancel()
			b := &Bot{DB: db, Delivery: botIntentTestSettings(), Host: appclient.Host{
				UserToken: func(context.Context, string) (string, error) {
					switch test.parent {
					case parentCancel:
						cancel()
					case parentDeadline:
						<-ctx.Done()
					}
					return "", test.failure
				},
			}}
			err = b.continueBotIntent(ctx, botdelivery.Intent{
				Owner: "owner", Reference: botdelivery.Reference{Kind: botdelivery.IdentityIntent},
			})
			if test.parent == "" {
				require.NoError(t, ctx.Err(), "returned failure is classified while the parent remains live")
			}
			switch {
			case test.schedule:
				require.ErrorIs(t, err, io.EOF, "provider failure is preserved")
				require.ErrorIs(t, err, core.ErrDatabase, "ordinary unavailability still schedules a durable retry")
				require.Positive(t, connects.Load())
			case core.IsDatabaseFailure(test.failure):
				require.Equal(t, core.ErrDatabase, err, "positive SQL failure outranks cancellation")
				require.Zero(t, connects.Load())
			default:
				require.ErrorIs(t, err, test.want)
				require.False(t, core.IsDatabaseFailure(err))
				require.Zero(t, connects.Load(), "cancellation or deadline must not run retry-scheduling SQL")
			}
		})
	}
}

func TestBackgroundDatabaseContinuationBatchStopsOnSQLFailure(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		"-- name: GetBotDeliveryIntent", "SELECT pg_advisory_xact_lock(hashtextextended($1,81081))",
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			testContinuationBatchSQLFailure(t, stage)
		})
	}
}

func testContinuationBatchSQLFailure(t *testing.T, stage string) {
	t.Helper()
	db := foodPendingDatabase(t)
	b := botDeliveryTestBot(db)
	var refs []delivery.Reference
	for _, operation := range []string{"identity:first", "identity:second"} {
		queued, err := b.enqueueBotIntent(t.Context(), "", 101, operation, "unavailable",
			botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
				Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
		require.NoError(t, err)
		refs = append(refs, queued.Reference)
		intent, err := botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
		require.NoError(t, err)
		attempt, ready, err := b.beginBotIntent(t.Context(), intent, botRenderedDelivery{})
		require.NoError(t, err)
		require.True(t, ready)
		require.NoError(t, b.finishBotIntent(t.Context(), attempt,
			delivery.Outcome{Kind: delivery.Succeeded, MessageID: 900}, botdelivery.Continuation{}, false))
	}
	fault := &receiptDatabaseFault{prefix: stage}
	config := db.Config()
	config.ConnConfig.Tracer = fault
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	b.DB = broken
	b.Host.LocalBotDelivery.Service.DB = broken
	require.ErrorIs(t, b.ContinueBotIntentReceipts(t.Context()), core.ErrDatabase)
	require.True(t, fault.fired)
	require.NoError(t, fault.closeError)
	for _, ref := range refs {
		pending, readErr := botdelivery.Read(t.Context(), db, b.Delivery.BotID, ref, false)
		require.NoError(t, readErr)
		require.False(t, pending.ContinuationDone, "later receipts must wait for a fresh runtime")
	}
	b.DB = db
	b.Host.LocalBotDelivery.Service.DB = db
	require.NoError(t, b.ContinueBotIntentReceipts(t.Context()))
	for _, ref := range refs {
		finished, readErr := botdelivery.Read(t.Context(), db, b.Delivery.BotID, ref, false)
		require.NoError(t, readErr)
		require.True(t, finished.ContinuationDone)
		require.EqualValues(t, 900, finished.MessageID)
	}
}

type receiptDatabaseFault struct {
	prefix     string
	fired      bool
	closeError error
}

func (fault *receiptDatabaseFault) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if !fault.fired && strings.HasPrefix(data.SQL, fault.prefix) {
		fault.fired = true
		fault.closeError = conn.Close(ctx)
	}
	return ctx
}

func (*receiptDatabaseFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestBackgroundDatabaseReceiptPersistenceTransport(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"begin", "-- name: LockBotDeliveryIntent", "UPDATE bot.delivery_intents SET state=$4", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			b := botDeliveryTestBot(db)
			queued, err := b.enqueueBotIntent(t.Context(), "", 101, "identity:receipt", "unavailable",
				botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
					Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
			require.NoError(t, err)
			intent, err := botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
			require.NoError(t, err)
			attempt, ready, err := b.beginBotIntent(t.Context(), intent, botRenderedDelivery{})
			require.NoError(t, err)
			require.True(t, ready)
			fault := &receiptDatabaseFault{prefix: stage}
			config := db.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			b.DB = broken
			outcome := delivery.Outcome{Kind: delivery.Succeeded, MessageID: 900}
			err = b.finishBotIntent(t.Context(), attempt, outcome, botdelivery.Continuation{}, false)
			require.True(t, fault.fired, "transport fault must reach the selected receipt SQL boundary")
			require.NoError(t, fault.closeError)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.EqualError(t, err, core.ErrDatabase.Error())
			pending, err := botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
			require.NoError(t, err)
			require.Equal(t, delivery.Sending, pending.State)
			require.Zero(t, pending.MessageID)
			b.DB = db
			require.NoError(t, b.finishBotIntent(t.Context(), attempt, outcome, botdelivery.Continuation{}, false))
			finished, err := botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
			require.NoError(t, err)
			require.Equal(t, delivery.Succeeded, finished.State)
			require.EqualValues(t, 900, finished.MessageID)
			require.Equal(t, attempt.Attempt, finished.Attempt)
		})
	}
}

func TestBackgroundDatabaseInboxResultAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Equal(t, core.ErrDatabase, inboxDatabaseError(ctx, &pgconn.PgError{Code: "08006"}))
	require.Equal(t, core.ErrDatabase, inboxDatabaseError(ctx, io.EOF))
	require.ErrorIs(t, inboxDatabaseError(ctx, pgx.ErrNoRows), pgx.ErrNoRows)
	require.NoError(t, inboxDatabaseError(ctx, nil))
	require.ErrorIs(t, inboxDatabaseError(ctx, context.Canceled), context.Canceled)
	require.ErrorIs(t, inboxDatabaseError(ctx, context.DeadlineExceeded), core.ErrDatabase)
	expired, expire := context.WithDeadline(t.Context(), time.Time{})
	defer expire()
	require.ErrorIs(t, expired.Err(), context.DeadlineExceeded)
	err := inboxDatabaseError(expired, context.DeadlineExceeded)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestBackgroundDatabaseRecoveryTransportPreservesIntent(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"begin", "SELECT operation_key,effect_key,CASE WHEN", "UPDATE bot.delivery_intents SET last_uncertain_attempt", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			b := botDeliveryTestBot(db)
			queued, err := b.enqueueBotIntent(t.Context(), "", 101, "identity:recovery", "unavailable",
				botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
					Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
			require.NoError(t, err)
			intent, err := botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
			require.NoError(t, err)
			attempt, ready, err := b.beginBotIntent(t.Context(), intent, botRenderedDelivery{})
			require.NoError(t, err)
			require.True(t, ready)
			fault := &receiptDatabaseFault{prefix: stage}
			config := db.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			b.DB = broken
			require.Equal(t, core.ErrDatabase, b.dispatchQueuedDeliveries(t.Context()))
			require.True(t, fault.fired)
			require.NoError(t, fault.closeError)
			require.NoError(t, db.Ping(t.Context()))
			pending, err := botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
			require.NoError(t, err)
			require.Equal(t, delivery.Sending, pending.State)
			require.Zero(t, pending.MessageID)
			b.DB = db
			require.NoError(t, b.RecoverBotIntents(t.Context()))
			pending, err = botdelivery.Read(t.Context(), db, b.Delivery.BotID, queued.Reference, false)
			require.NoError(t, err)
			require.Equal(t, delivery.Deferred, pending.State)
			require.Equal(t, attempt.Attempt, pending.Attempt)
			require.NoError(t, b.RecoverBotIntents(t.Context()), "empty recovery is a normal control")
		})
	}
}
