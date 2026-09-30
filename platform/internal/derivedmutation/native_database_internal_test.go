package derivedmutation

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative/dbgen"
)

type nativeDatabaseTx struct {
	pgx.Tx

	beginErr    error
	rowErr      error
	rollbackErr error
	rolledBack  bool
}

func (tx *nativeDatabaseTx) Begin(context.Context) (pgx.Tx, error) { return tx, tx.beginErr }
func (tx *nativeDatabaseTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return nativeDatabaseRow{err: tx.rowErr}
}
func (tx *nativeDatabaseTx) Rollback(context.Context) error {
	tx.rolledBack = true
	return tx.rollbackErr
}

type nativeDatabaseRow struct{ err error }

func (row nativeDatabaseRow) Scan(...any) error { return row.err }

func TestNativeDatabaseSavepointFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		beginErr    error
		captureErr  error
		rollbackErr error
		want        error
		database    bool
	}{
		{name: "begin EOF", beginErr: io.EOF, want: core.ErrDatabase, database: true},
		{name: "begin cancellation", beginErr: context.Canceled, want: context.Canceled},
		{name: "rollback EOF after refusal", captureErr: pgx.ErrNoRows, rollbackErr: io.EOF, want: core.ErrDatabase, database: true},
		{name: "SQL then canceled rollback", captureErr: io.EOF, rollbackErr: context.Canceled, want: core.ErrDatabase, database: true},
		{name: "canceled capture and rollback", captureErr: context.Canceled, rollbackErr: context.Canceled, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tx := &nativeDatabaseTx{beginErr: test.beginErr, rowErr: test.captureErr, rollbackErr: test.rollbackErr}
			_, err := (NativeRegistrationResolver{}).captureCandidateAttempt(t.Context(), tx,
				dbgen.PendingNativeCandidatesRow{}, registrationnative.Envelope{
					Owner: "alice", Command: passbooking.Command{Name: "solo", Event: "dance", Key: "native-key"},
				})
			require.ErrorIs(t, err, test.want)
			require.Equal(t, test.database, core.IsDatabaseFailure(err))
			require.False(t, nativeRegistrationRefusal(err))
			if errors.Is(test.rollbackErr, io.EOF) {
				var problem *core.ProblemError
				require.NotErrorAs(t, err, &problem)
			}
			require.Equal(t, test.beginErr == nil, tx.rolledBack)
			require.NotContains(t, err.Error(), "EOF")
		})
	}
}
