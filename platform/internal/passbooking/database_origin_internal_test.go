package passbooking

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

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

func (tx originFailureTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, tx.err
}

func (tx originFailureTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.err
}

type originFailureRow struct{ err error }

func (row originFailureRow) Scan(...any) error { return row.err }

func TestRegistrationSQLOrigins(t *testing.T) {
	t.Parallel()
	for _, input := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(input.Error(), func(t *testing.T) {
			t.Parallel()
			ctx := receiptParentContext(t, input)
			tx := originFailureTx{err: input}
			want := input
			if errors.Is(input, io.EOF) {
				want = core.ErrDatabase
			}
			_, err := (Service{}).PrepareAdmissionInTx(
				t.Context(), tx, "owner", Command{Name: "solo", Event: "dance", Key: "key"},
			)
			require.ErrorIs(t, err, want)
			_, err = readEvent(ctx, tx, "dance")
			require.ErrorIs(t, err, want)
			_, err = readBookings(t.Context(), tx, "dance")
			require.ErrorIs(t, err, want)
			_, err = authorize(ctx, tx, "owner", "solo", "dance")
			require.ErrorIs(t, err, want)
			require.ErrorIs(t, LockMutationEvents(ctx, tx, []string{"dance"}), want)
			require.ErrorIs(t, refreshRegistrationTurns(t.Context(), tx, "dance", time.Minute, time.Now()), want)
		})
	}
}

func TestRegistrationSQLAbsenceKeepsDomainMeaning(t *testing.T) {
	t.Parallel()
	tx := originFailureTx{err: pgx.ErrNoRows}
	_, err := readEvent(t.Context(), tx, "missing")
	require.Equal(t, conflict("pass_event_unknown"), err)
	_, err = authorize(t.Context(), tx, "missing", "solo", "dance")
	require.Equal(t, forbidden(), err)
	err = lockRegistrationProfile(t.Context(), tx, "owner", "solo", false)
	require.Equal(t, conflict("pass_profile_required"), err)
	prepared := PreparedCommand{tx: tx, command: Command{Event: "dance"}, actor: "owner"}
	_, found, err := prepared.existingAdmission(t.Context())
	require.NoError(t, err)
	require.False(t, found)
}
