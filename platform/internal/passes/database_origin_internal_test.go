package passes

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type originFailureTx struct {
	pgx.Tx

	err error
}

func (tx originFailureTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return originFailureRow{err: tx.err}
}

func (tx originFailureTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.err
}

type originFailureRow struct{ err error }

func (row originFailureRow) Scan(...any) error { return row.err }

func TestProfileSQLOrigins(t *testing.T) {
	t.Parallel()
	for _, input := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(input.Error(), func(t *testing.T) {
			t.Parallel()
			want := input
			if errors.Is(input, io.EOF) {
				want = core.ErrDatabase
			}
			tx := originFailureTx{err: input}
			command := Command{Name: "set", Field: "role", Value: "leader", Key: "key", Origin: "manual"}
			_, err := (Service{}).PrepareInTx(t.Context(), tx, "owner", command)
			require.ErrorIs(t, err, want)
			_, err = scan(originFailureRow{err: input})
			require.ErrorIs(t, err, want)
			require.ErrorIs(t, recordChange(t.Context(), tx, "owner", 1, command), want)
		})
	}
}

func TestProfileSQLAbsenceAndValidation(t *testing.T) {
	t.Parallel()
	_, err := scan(originFailureRow{err: pgx.ErrNoRows})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	command := Command{Name: "set", Field: "role", Value: "leader", Key: "key", Origin: "manual"}
	_, err = (Service{}).PrepareInTx(t.Context(), originFailureTx{err: pgx.ErrNoRows}, "missing", command)
	require.Equal(t, problem(http.StatusForbidden, "forbidden"), err)
	_, err = (Service{}).PrepareInTx(t.Context(), nil, "owner", Command{})
	require.Equal(t, problem(http.StatusBadRequest, "pass_profile_invalid"), err)
}
