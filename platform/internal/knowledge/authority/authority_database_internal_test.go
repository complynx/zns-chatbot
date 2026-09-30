package authority

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type authorityDatabaseTx struct {
	pgx.Tx

	err error
}

func (tx authorityDatabaseTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return authorityDatabaseRow{err: tx.err}
}

type authorityDatabaseRow struct{ err error }

func (row authorityDatabaseRow) Scan(...any) error { return row.err }

func TestAuthorityDatabaseLeaves(t *testing.T) {
	t.Parallel()
	for _, failure := range []struct {
		name string
		err  error
		want error
	}{
		{name: "EOF", err: io.EOF, want: core.ErrDatabase},
		{name: "cancellation", err: context.Canceled, want: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: core.ErrDatabaseSerialization},
	} {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			tx := authorityDatabaseTx{err: failure.err}
			require.ErrorIs(t, LockPermission(t.Context(), tx, "alice", "dance", Review), failure.want)
			require.ErrorIs(t, LockSharedGate(t.Context(), tx), failure.want)
			for _, authority := range []ReadAuthority{
				{Kind: PrivateMemory},
				{Kind: DerivedMemory, Namespace: "private", Owner: "alice", Topic: "topic", Key: "key", SourceKind: "memo", Version: 1},
				{Kind: DerivedProposal, Owner: "alice", ProposalID: 1},
			} {
				_, err := lockRead(t.Context(), tx, "alice", authority)
				require.ErrorIs(t, err, failure.want)
			}
		})
	}
}

func TestAuthorityDatabaseAbsenceAndInvalidEvidence(t *testing.T) {
	t.Parallel()
	tx := authorityDatabaseTx{err: pgx.ErrNoRows}
	allowed, err := lockRead(t.Context(), tx, "alice", ReadAuthority{Kind: PrivateMemory})
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = lockRead(t.Context(), tx, "alice", ReadAuthority{Kind: Review, Scope: "dance"})
	require.NoError(t, err)
	require.False(t, allowed)
	require.ErrorIs(t, LockSharedGate(t.Context(), tx), pgx.ErrNoRows)
	_, err = lockRead(t.Context(), tx, "alice", ReadAuthority{})
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
}
