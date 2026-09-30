package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// connectionLost has the shape of an established-connection read failure:
// neither a PostgreSQL statement error nor a connect error.
func connectionLost() error { return &net.OpError{Op: "read", Net: "tcp", Err: io.EOF} }

type scriptedRow func(dest ...any) error

func (r scriptedRow) Scan(dest ...any) error { return r(dest...) }

func rowValues(values ...any) scriptedRow {
	return func(dest ...any) error {
		for i, value := range values {
			reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
		}
		return nil
	}
}

func rowError(err error) scriptedRow { return func(...any) error { return err } }

// scriptedTx injects results at the pgx.Tx SQL interface. Unscripted calls panic.
type scriptedTx struct {
	pgx.Tx

	exec error
	rows []scriptedRow
}

func (t *scriptedTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, t.exec
}

func (t *scriptedTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(t.rows) == 0 {
		panic("unscripted query")
	}
	row := t.rows[0]
	t.rows = t.rows[1:]
	return row
}

func eventRow() scriptedRow {
	return rowValues("event", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), json.RawMessage(`{}`), map[string]Extra{})
}

func createCommand() Command {
	return Command{EventID: "event", Name: actionCreate, Key: "key", Origin: "manual"}
}

func requireSanitizedDatabaseFailure(t *testing.T, err error) {
	t.Helper()
	require.True(t, core.IsDatabaseFailure(err))
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotErrorIs(t, err, io.EOF, "driver cause is not retained")
	var driver *pgconn.PgError
	require.NotErrorAs(t, err, &driver)
	var public *core.ProblemError
	require.NotErrorAs(t, err, &public, "a SQL failure is not a domain outcome")
	require.EqualError(t, err, core.ErrDatabase.Error())
}

func requireProblem(t *testing.T, err error, code string) {
	t.Helper()
	var public *core.ProblemError
	require.ErrorAs(t, err, &public)
	require.Equal(t, code, public.Code)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestPrepareInTxMarksConnectionLossAtEachSQLStage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		tx   *scriptedTx
	}{
		{"event lock exec", &scriptedTx{exec: connectionLost()}},
		{"actor scan", &scriptedTx{rows: []scriptedRow{rowError(io.ErrUnexpectedEOF)}}},
		{"event scan after authorized actor", &scriptedTx{rows: []scriptedRow{rowValues(true), rowError(connectionLost())}}},
		{"receipt scan", &scriptedTx{rows: []scriptedRow{rowValues(true), eventRow(), rowError(connectionLost())}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			prepared, err := Service{}.PrepareInTx(t.Context(), test.tx, "actor", createCommand())
			require.Nil(t, prepared)
			requireSanitizedDatabaseFailure(t, err)
		})
	}
}

func TestPrepareInTxKeepsAbsenceAndCancellationOutcomes(t *testing.T) {
	t.Parallel()
	_, err := Service{}.PrepareInTx(t.Context(), &scriptedTx{rows: []scriptedRow{rowError(pgx.ErrNoRows)}},
		"actor", createCommand())
	requireProblem(t, err, "forbidden")

	_, err = Service{}.PrepareInTx(
		t.Context(),
		&scriptedTx{rows: []scriptedRow{rowValues(true), rowError(pgx.ErrNoRows)}},
		"actor",
		createCommand(),
	)
	requireProblem(t, err, "event_not_found")

	canceled := fmt.Errorf("read: %w", context.Canceled)
	_, err = Service{}.PrepareInTx(t.Context(), &scriptedTx{rows: []scriptedRow{rowError(canceled)}},
		"actor", createCommand())
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))

	prepared, err := Service{}.PrepareInTx(
		t.Context(),
		&scriptedTx{
			rows: []scriptedRow{rowValues(true), eventRow(), rowError(pgx.ErrNoRows)},
		},
		"actor",
		createCommand(),
	)
	require.NoError(t, err)
	_, found := prepared.Replay()
	require.False(t, found, "a missing receipt remains a new command")
}

func TestDeliveryLockDropsDriverDiagnostics(t *testing.T) {
	t.Parallel()
	private := &pgconn.PgError{Code: "57P01", Message: "private diagnostic"}
	err := LockDeliveryPaymentInTx(
		t.Context(),
		&scriptedTx{rows: []scriptedRow{rowError(private)}},
		"actor",
		"event",
		"order",
	)
	requireSanitizedDatabaseFailure(t, err)
	encoded, encodeErr := json.Marshal(err)
	require.NoError(t, encodeErr)
	require.NotContains(t, string(encoded)+err.Error(), "private")

	err = LockDeliveryPaymentInTx(
		t.Context(),
		&scriptedTx{rows: []scriptedRow{rowError(pgx.ErrNoRows)}},
		"actor",
		"event",
		"order",
	)
	requireProblem(t, err, "order_not_found")
}

// Negative control: EOF-shaped failures without a SQL origin keep their meaning.
func TestNonSQLFailureIsNotClassifiedAsDatabase(t *testing.T) {
	t.Parallel()
	require.False(t, core.IsDatabaseFailure(connectionLost()), "a transport EOF alone has no SQL provenance")

	// The row itself succeeds; decoding its stored JSON then hits end of input.
	truncated := scriptedRow(func(dest ...any) error {
		reflect.ValueOf(dest[8]).Elem().Set(reflect.ValueOf(json.RawMessage(`{`)))
		return nil
	})
	_, err := scanRefund(truncated)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))

	// The same EOF at the row Scan is a SQL result.
	_, err = scanRefund(rowError(connectionLost()))
	requireSanitizedDatabaseFailure(t, err)
}
