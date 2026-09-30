package botdelivery

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type receiptSQLFailure struct {
	pgx.Tx

	err error
}

func (tx receiptSQLFailure) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.err
}

func (tx receiptSQLFailure) QueryRow(context.Context, string, ...any) pgx.Row {
	return readDatabaseRow(func(...any) error { return tx.err })
}

func TestReceiptProjectionDatabaseFailureAndBinding(t *testing.T) {
	t.Parallel()
	s := Service{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, kind := range []string{"workflow_card", "order_card", "pass_card", "massage_card", familyPassRedaction} {
		i := Intent{State: delivery.Succeeded, MessageID: 1, Receipt: Continuation{Kind: kind}}
		require.ErrorIs(t, s.projectReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, i), core.ErrDatabase)
		require.ErrorIs(t, s.projectReceipt(t.Context(), receiptSQLFailure{err: context.Canceled}, i), core.ErrDatabase)
		require.ErrorIs(t, s.projectReceipt(ctx, receiptSQLFailure{err: context.Canceled}, i), context.Canceled)
	}
	require.ErrorIs(t, s.projectReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, Intent{}), ErrBinding)
}

func TestRenderedTargetDatabaseFailureAndAbsentView(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"workflow", familyPasses, familyMassage, "order"} {
		i := Intent{Reference: Reference{Family: family}}
		require.ErrorIs(t, lockRenderedTarget(t.Context(), receiptSQLFailure{err: io.EOF}, i, 0), core.ErrDatabase)
		require.NoError(t, lockRenderedTarget(t.Context(), receiptSQLFailure{err: pgx.ErrNoRows}, i, 0))
		require.ErrorIs(t, lockRenderedTarget(t.Context(), receiptSQLFailure{err: pgx.ErrNoRows}, i, 1), pgx.ErrNoRows)
	}
}

// admissionSQLFailure permits actor prelocks and fails the actual binding read.
type admissionSQLFailure struct {
	pgx.Tx

	err     error
	pending bool
}

type admissionEmptyRows struct{ pgx.Rows }

func (admissionEmptyRows) Close()     {}
func (admissionEmptyRows) Next() bool { return false }
func (admissionEmptyRows) Err() error { return nil }

func (admissionSQLFailure) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return admissionEmptyRows{}, nil
}

func (tx admissionSQLFailure) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.pending {
		return readDatabaseRow(func(dest ...any) error {
			*dest[0].(*bool) = true
			return nil
		})
	}
	return readDatabaseRow(func(...any) error { return tx.err })
}

func (tx admissionSQLFailure) Commit(context.Context) error { return tx.err }

func TestAdmissionSQLBoundariesPreserveProvenance(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, expire := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	t.Cleanup(expire)
	cases := []struct {
		name          string
		ctx           context.Context
		failure, want error
	}{
		{"live_cancel", t.Context(), context.Canceled, core.ErrDatabase},
		{"live_deadline", t.Context(), context.DeadlineExceeded, core.ErrDatabase},
		{"caller_cancel", cancelled, context.Canceled, context.Canceled},
		{"caller_deadline", expired, context.DeadlineExceeded, context.DeadlineExceeded},
		{"cancelled_SQL", cancelled, &pgconn.PgError{Code: "42601", Message: "private statement"}, core.ErrDatabase},
		{
			"cancelled_serialization",
			cancelled,
			&pgconn.PgError{Code: "40001", Message: "private statement"},
			core.ErrDatabaseSerialization,
		},
		{"absence", t.Context(), pgx.ErrNoRows, pgx.ErrNoRows},
	}
	boundaries := []struct {
		name string
		run  func(context.Context, admissionSQLFailure) error
	}{
		{"actor_chat", func(ctx context.Context, tx admissionSQLFailure) error {
			return (Service{}).lockPayloadActors(ctx, tx, Intent{Owner: "alice"}, nil, &familyRead{})
		}},
		{"pending_receipt", func(ctx context.Context, tx admissionSQLFailure) error {
			_, _, err := (Service{}).beginAttempt(ctx, tx, Intent{Reference: Reference{Kind: CardIntent}}, 0)
			return err
		}},
		{"pending_commit", func(ctx context.Context, tx admissionSQLFailure) error {
			tx.pending = true
			_, _, err := (Service{}).beginAttempt(ctx, tx, Intent{Reference: Reference{Kind: CardIntent}}, 0)
			return err
		}},
		{"pass_view", func(ctx context.Context, tx admissionSQLFailure) error {
			return lockViewBinding(ctx, tx, Intent{Reference: Reference{Family: familyPasses}}, familyRead{})
		}},
		{"massage_view", func(ctx context.Context, tx admissionSQLFailure) error {
			return lockViewBinding(ctx, tx, Intent{Reference: Reference{Family: familyMassage}}, familyRead{})
		}},
		{"massage_read", func(ctx context.Context, tx admissionSQLFailure) error {
			_, _, err := (Service{}).prepareFamily(ctx, tx, Intent{Reference: Reference{Family: familyMassage}}, nil)
			return err
		}},
		{"rendered_target", func(ctx context.Context, tx admissionSQLFailure) error {
			return lockRenderedTarget(ctx, tx, Intent{Reference: Reference{Family: familyPasses}}, 1)
		}},
	}
	for _, boundary := range boundaries {
		t.Run(boundary.name, func(t *testing.T) {
			t.Parallel()
			for _, testcase := range cases {
				t.Run(testcase.name, func(t *testing.T) {
					t.Parallel()
					tx := admissionSQLFailure{err: testcase.failure}
					err := boundary.run(testcase.ctx, tx)
					require.ErrorIs(t, err, testcase.want)
					require.NotContains(t, err.Error(), "private statement")
				})
			}
		})
	}
}
