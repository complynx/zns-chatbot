package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type adminDatabaseTx struct {
	pgx.Tx

	err       error
	rowErrors []error
	rows      pgx.Rows
	execs     int
}

func (tx *adminDatabaseTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(tx.rowErrors) > 0 {
		err := tx.rowErrors[0]
		tx.rowErrors = tx.rowErrors[1:]
		return adminDatabaseRow{err: err}
	}
	return adminDatabaseRow{err: tx.err}
}

func (tx *adminDatabaseTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.rows != nil {
		return tx.rows, nil
	}
	return nil, tx.err
}

func (tx *adminDatabaseTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execs++
	return pgconn.CommandTag{}, tx.err
}

func (tx *adminDatabaseTx) Begin(context.Context) (pgx.Tx, error) { return nil, tx.err }

type adminDatabaseRow struct{ err error }

func (row adminDatabaseRow) Scan(...any) error { return row.err }

type adminDatabaseRows struct {
	pgx.Rows

	scanErr error
	endErr  error
	next    bool
}

func (rows *adminDatabaseRows) Close() {}
func (rows *adminDatabaseRows) Next() bool {
	next := rows.next
	rows.next = false
	return next
}
func (rows *adminDatabaseRows) Scan(...any) error { return rows.scanErr }
func (rows *adminDatabaseRows) Err() error        { return rows.endErr }

func TestAdminDatabaseParticipantStream(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		rows adminDatabaseRows
		want error
	}{
		{name: "scan", rows: adminDatabaseRows{next: true, scanErr: io.EOF}, want: core.ErrDatabase},
		{name: "iteration", rows: adminDatabaseRows{endErr: io.EOF}, want: core.ErrDatabase},
		{name: "cancelled", rows: adminDatabaseRows{endErr: context.Canceled}, want: context.Canceled},
		{name: "empty", want: conflict("pass_payment_stale")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (&snapshot{}).paymentParticipants(t.Context(), &adminDatabaseTx{rows: &test.rows}, Command{})
			require.Equal(t, test.want, err)
		})
	}
}

func TestAdminDatabaseOrigins(t *testing.T) {
	t.Parallel()
	for _, input := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(input.Error(), func(t *testing.T) {
			t.Parallel()
			ctx := receiptParentContext(t, input)
			tx := &adminDatabaseTx{err: input}
			want := input
			if errors.Is(input, io.EOF) {
				want = core.ErrDatabase
			}
			s := &snapshot{}
			c := AdminAssignment{Event: "dance", Key: "key", Target: "owner", Create: &AdminCreate{}}
			require.ErrorIs(t, authorizeTakeover(ctx, tx, "admin", "dance"), want)
			require.ErrorIs(t, authorizeBatchCancel(ctx, tx, "admin", "dance"), want)
			require.ErrorIs(t, lockAdminUsers(t.Context(), tx, "admin", "owner"), want)
			_, err := lockAdminProfile(t.Context(), tx, c)
			require.ErrorIs(t, err, want)
			_, _, err = adminReplay(t.Context(), tx, "admin", c, "key", "digest", nil)
			require.ErrorIs(t, err, want)
			err = recordAdminAssignment(t.Context(), tx, "admin", c, "key", "digest", nil,
				AdminAssignmentResult{}, time.Now())
			require.ErrorIs(t, err, want)
			_, err = assignmentTransitions(ctx, tx, "admin", c)
			require.ErrorIs(t, err, want)
			require.ErrorIs(t, operationTarget(t.Context(), tx, "dance", commandAdminAssign, "owner"), want)
			_, err = s.paymentParticipants(t.Context(), tx, Command{})
			require.ErrorIs(t, err, want)
			b := &Booking{State: assigned}
			require.ErrorIs(t, s.submitProof(t.Context(), tx, b, Command{ProofID: strings.Repeat("a", 64)}), want)
			batch := &RuntimeBatchState{}
			require.ErrorIs(t, batch.Persist(t.Context(), tx, RuntimeBatch{}, nil), want)
			require.ErrorIs(t, batch.LockRuntimeBatchInTx(ctx, tx), want)
			err = (Service{}).executeRuntimeBatchItem(t.Context(), tx, "admin", commandAdminAssign, &RuntimeBatchItem{})
			require.ErrorIs(t, err, want)
		})
	}
}

func TestAdminDatabaseRoleFallbackAndAbsence(t *testing.T) {
	t.Parallel()
	checks := []func(context.Context, pgx.Tx, string, string) error{authorizeTakeover, authorizeBatchCancel}
	for _, authorize := range checks {
		tx := &adminDatabaseTx{rowErrors: []error{pgx.ErrNoRows, io.EOF}}
		require.ErrorIs(t, authorize(t.Context(), tx, "admin", "dance"), core.ErrDatabase)
		tx.rowErrors = []error{pgx.ErrNoRows, pgx.ErrNoRows}
		require.Equal(t, forbidden(), authorize(t.Context(), tx, "admin", "dance"))
	}
	tx := &adminDatabaseTx{err: pgx.ErrNoRows}
	_, found, err := adminReplay(t.Context(), tx, "admin", AdminAssignment{}, "key", "digest", nil)
	require.NoError(t, err)
	require.False(t, found)
	_, err = assignmentTransitions(t.Context(), tx, "admin", AdminAssignment{})
	require.Equal(t, conflict("source_stale"), err)
	require.Equal(t, forbidden(), operationTarget(t.Context(), tx, "dance", commandAdminAssign, "owner"))
	err = (&snapshot{}).submitProof(t.Context(), tx, &Booking{State: assigned},
		Command{ProofID: strings.Repeat("a", 64)})
	require.Equal(t, forbidden(), err)
}

func TestAdminDatabaseInfrastructureDoesNotPersistRejection(t *testing.T) {
	t.Parallel()
	tx := &adminDatabaseTx{}
	b := &RuntimeBatchState{plan: runtimeBatchPlan{Items: []RuntimeBatchItem{
		{Outcome: AdminBatchOutcome{Status: AdminBatchNotAttempted}},
	}}}
	require.ErrorIs(t, b.SaveOutcome(t.Context(), tx, 0, core.ErrDatabase), core.ErrDatabase)
	require.Zero(t, tx.execs)
	require.Equal(t, AdminBatchInterrupted, b.Items()[0].Outcome.Status)
	require.NoError(t, b.SaveOutcome(t.Context(), tx, 0, conflict("pass_booking_stale")))
	require.Equal(t, 1, tx.execs)
	require.Equal(t, AdminBatchRejected, b.Items()[0].Outcome.Status)
	require.Equal(t, "pass_booking_stale", b.Items()[0].Outcome.Code)
}

type adminDatabaseReplayTx struct {
	pgx.Tx

	reads int
}

type adminDatabaseScanRow func(...any) error

func (scan adminDatabaseScanRow) Scan(dest ...any) error { return scan(dest...) }

func (tx *adminDatabaseReplayTx) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.reads++
	return adminDatabaseScanRow(func(dest ...any) error {
		if tx.reads == 1 {
			*dest[0].(*string) = "digest"
		} else {
			*dest[0].(*int) = 1
			*dest[1].(*[]byte) = []byte("{")
		}
		return nil
	})
}

func TestAdminDatabaseReplayJSONKeepsNonSQLOrigin(t *testing.T) {
	t.Parallel()
	_, _, err := adminReplay(t.Context(), &adminDatabaseReplayTx{}, "admin", AdminAssignment{}, "key", "digest", nil)
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
	require.False(t, core.IsDatabaseFailure(err))
}
