package delivery_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type failingSQLTx struct {
	pgx.Tx

	failAt            int
	calls             int
	err               error
	queueState        string
	missingFirstRead  bool
	projectionMissing bool
}

func (tx *failingSQLTx) nextError() error {
	tx.calls++
	if tx.calls == tx.failAt {
		return tx.err
	}
	return nil
}

func (tx *failingSQLTx) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	tag := "UPDATE 1"
	if tx.projectionMissing && strings.Contains(query, "-- name: ProjectDeliveryEntry") {
		tag = "UPDATE 0"
	}
	return pgconn.NewCommandTag(tag), tx.nextError()
}

func (tx *failingSQLTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	err := tx.nextError()
	if err == nil && tx.missingFirstRead && tx.calls == 3 && strings.Contains(query, "-- name: ReadDeliveryEntry") {
		err = pgx.ErrNoRows
	}
	return pacingSQLRow{err: err, queueState: tx.queueState}
}

func (tx *failingSQLTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, tx.nextError()
}

type pacingSQLRow struct {
	err        error
	queueState string
}

func (row pacingSQLRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, value := range dest {
		switch target := value.(type) {
		case *pgtype.Timestamptz:
			*target = pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}
		case *pgtype.Bool:
			*target = pgtype.Bool{Bool: true, Valid: true}
		case *int64:
			if row.queueState != "" && index != 5 {
				*target = 1
			}
		case *string:
			if row.queueState != "" && len(dest) == 10 {
				fields := map[int]string{
					1: "orders",
					2: "order",
					3: "receipt",
					4: "1",
					7: "interactive",
					8: row.queueState,
				}
				*target = fields[index]
			}
		}
	}
	return nil
}

func TestSQLOriginErrorsInDelivery(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{BotID: 1, BotInterval: time.Second, ChatInterval: time.Second, Fallback: time.Second}
	ref := delivery.Reference{Owner: delivery.Orders, Key: "order", Effect: "receipt"}
	destination := delivery.Destination{Chat: "1"}
	cases := []struct {
		name  string
		calls int
		run   func(context.Context, pgx.Tx) error
	}{
		{"register", 3, func(ctx context.Context, tx pgx.Tx) error {
			_, err := delivery.Register(ctx, tx, 1, ref, destination, delivery.Interactive)
			return err
		}},
		{"read", 1, func(ctx context.Context, tx pgx.Tx) error {
			_, err := delivery.ReadReference(ctx, tx, 1, ref)
			return err
		}},
		{
			"candidates",
			1,
			func(ctx context.Context, tx pgx.Tx) error { _, err := delivery.Candidates(ctx, tx, 1, 1); return err },
		},
		{"reserve", 7, func(ctx context.Context, tx pgx.Tx) error {
			_, err := delivery.Reserve(ctx, tx, settings, destination)
			return err
		}},
		{"reserve_control", 6, func(ctx context.Context, tx pgx.Tx) error {
			_, err := delivery.ReserveControl(ctx, tx, settings)
			return err
		}},
		{"schedule_control", 4, func(ctx context.Context, tx pgx.Tx) error {
			_, _, err := delivery.ScheduleControl(
				ctx,
				tx,
				settings,
				delivery.Outcome{Kind: delivery.Paused, Reason: "telegram_forbidden"},
			)
			return err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for step := 1; step <= test.calls; step++ {
				for _, fault := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
					tx := &failingSQLTx{failAt: step, err: fault}
					want := fault
					if errors.Is(fault, io.EOF) {
						want = core.ErrDatabase
					}
					require.ErrorIs(t, test.run(t.Context(), tx), want, "SQL step %d", step)
					require.Equal(t, step, tx.calls)
				}
			}
		})
	}
}

func TestDeliveryDomainAndNoRowsErrorsSurvive(t *testing.T) {
	t.Parallel()
	ref := delivery.Reference{Owner: delivery.Orders, Key: "order", Effect: "receipt"}
	tx := &failingSQLTx{failAt: 1, err: pgx.ErrNoRows}
	_, err := delivery.ReadReference(t.Context(), tx, 1, ref)
	require.ErrorIs(t, err, delivery.ErrQueueReference)
	require.NotErrorIs(t, err, core.ErrDatabase)
	_, err = delivery.Reserve(t.Context(), nil, delivery.Settings{}, delivery.Destination{Chat: "1"})
	require.ErrorIs(t, err, delivery.ErrSettings)
	require.NotErrorIs(t, err, core.ErrDatabase)
	err = delivery.Project(t.Context(), nil, 1, ref, delivery.Succeeded, time.Time{})
	require.ErrorIs(t, err, delivery.ErrQueueState)
	_, err = delivery.Register(
		t.Context(),
		&failingSQLTx{},
		1,
		ref,
		delivery.Destination{Chat: "1"},
		delivery.Interactive,
	)
	require.ErrorIs(t, err, delivery.ErrQueueBinding)
}

func TestQueueLifecycleSQLFailures(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{BotID: 1, BotInterval: time.Second, ChatInterval: time.Second, Fallback: time.Second}
	ref := delivery.Reference{Owner: delivery.Orders, Key: "order", Effect: "receipt"}
	cases := []struct {
		name  string
		calls int
		state delivery.Kind
		run   func(context.Context, pgx.Tx) error
	}{
		{"register_new", 6, delivery.Deferred, func(ctx context.Context, tx pgx.Tx) error {
			_, err := delivery.Register(ctx, tx, 1, ref, delivery.Destination{Chat: "1"}, delivery.Interactive)
			return err
		}},
		{"begin", 15, delivery.Deferred, func(ctx context.Context, tx pgx.Tx) error {
			_, err := delivery.Begin(ctx, tx, settings, ref)
			return err
		}},
		{"finish", 11, delivery.Sending, func(ctx context.Context, tx pgx.Tx) error {
			_, _, err := delivery.Finish(
				ctx,
				tx,
				settings,
				ref,
				delivery.Outcome{Kind: delivery.Paused, Reason: "telegram_forbidden"},
			)
			return err
		}},
		{"project", 4, delivery.Deferred, func(ctx context.Context, tx pgx.Tx) error {
			return delivery.Project(ctx, tx, 1, ref, delivery.Cancelled, time.Time{})
		}},
		{"lock_references", 3, delivery.Deferred, func(ctx context.Context, tx pgx.Tx) error {
			return delivery.LockReferences(ctx, tx, 1, []delivery.Reference{ref})
		}},
	}
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			registration := sample.name == "register_new"
			for step := 1; step <= sample.calls; step++ {
				tx := &failingSQLTx{
					failAt:           step,
					err:              io.EOF,
					queueState:       string(sample.state),
					missingFirstRead: registration,
				}
				err := sample.run(t.Context(), tx)
				require.ErrorIs(t, err, core.ErrDatabase, "SQL step %d", step)
				require.NotErrorIs(t, err, io.EOF)
				require.Equal(t, step, tx.calls)
			}
			tx := &failingSQLTx{queueState: string(sample.state), missingFirstRead: registration}
			require.NoError(t, sample.run(t.Context(), tx))
			require.Equal(t, sample.calls, tx.calls)
			if !registration {
				tx = &failingSQLTx{failAt: 1, err: pgx.ErrNoRows, queueState: string(sample.state)}
				require.ErrorIs(t, sample.run(t.Context(), tx), delivery.ErrQueueReference)
				tx = &failingSQLTx{failAt: 3, err: pgx.ErrNoRows, queueState: string(sample.state)}
				require.ErrorIs(t, sample.run(t.Context(), tx), pgx.ErrNoRows)
			}
		})
	}
}

func TestQueueLifecycleDomainAndSuccessResults(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{BotID: 1, BotInterval: time.Second, ChatInterval: time.Second, Fallback: time.Second}
	ref := delivery.Reference{Owner: delivery.Orders, Key: "order", Effect: "receipt"}
	tx := &failingSQLTx{queueState: string(delivery.Deferred), missingFirstRead: true}
	entry, err := delivery.Register(t.Context(), tx, 1, ref, delivery.Destination{Chat: "1"}, delivery.Interactive)
	require.NoError(t, err)
	require.Equal(t, ref, entry.Reference)
	require.Equal(t, int64(1), entry.Sequence)
	require.Equal(t, delivery.Deferred, entry.State)

	tx = &failingSQLTx{queueState: string(delivery.Deferred)}
	admission, err := delivery.Begin(t.Context(), tx, settings, ref)
	require.NoError(t, err)
	require.True(t, admission.Ready)

	tx = &failingSQLTx{queueState: string(delivery.Sending)}
	outcome := delivery.Outcome{Kind: delivery.Succeeded, MessageID: 42}
	result, _, err := delivery.Finish(t.Context(), tx, settings, ref, outcome)
	require.NoError(t, err)
	require.Equal(t, outcome, result)

	tx = &failingSQLTx{queueState: string(delivery.Succeeded)}
	err = delivery.Project(t.Context(), tx, 1, ref, delivery.Cancelled, time.Time{})
	require.ErrorIs(t, err, delivery.ErrQueueState)
	require.Equal(t, 3, tx.calls)
	require.NotErrorIs(t, err, core.ErrDatabase)

	tx = &failingSQLTx{queueState: string(delivery.Deferred), projectionMissing: true}
	err = delivery.Project(t.Context(), tx, 1, ref, delivery.Cancelled, time.Time{})
	require.ErrorIs(t, err, delivery.ErrQueueReference)
	require.Equal(t, 4, tx.calls)
	require.NotErrorIs(t, err, core.ErrDatabase)

	tx = &failingSQLTx{queueState: string(delivery.Deferred)}
	_, _, err = delivery.Finish(t.Context(), tx, settings, ref, outcome)
	require.ErrorIs(t, err, delivery.ErrQueueState)
	require.Equal(t, 3, tx.calls)
}
