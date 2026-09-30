package broadcastprofile_test

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/broadcastprofile"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type profileWriteTx struct {
	pgx.Tx

	err   error
	rows  string
	calls int
}

func (tx *profileWriteTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.calls++
	return pgconn.NewCommandTag(tx.rows), tx.err
}

func TestProfileWriteSQLFailureAndDomainOutcomes(t *testing.T) {
	t.Parallel()
	tx := &profileWriteTx{err: io.EOF}
	err := broadcastprofile.PassField(t.Context(), tx, "alice", "legal_name", "Synthetic")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
	require.Equal(t, 1, tx.calls)

	tx = &profileWriteTx{rows: "INSERT 0 0"}
	err = broadcastprofile.PassField(t.Context(), tx, "alice", "legal_name", "Synthetic")
	require.EqualError(t, err, "broadcast_profile_missing")
	require.False(t, core.IsDatabaseFailure(err))
	require.Equal(t, 1, tx.calls)

	tx = &profileWriteTx{rows: "INSERT 0 1"}
	err = broadcastprofile.PassField(t.Context(), tx, "alice", "invalid", "Synthetic")
	require.EqualError(t, err, "invalid_broadcast_profile_field")
	require.False(t, core.IsDatabaseFailure(err))
	require.Zero(t, tx.calls)
	require.NoError(t, broadcastprofile.PassField(t.Context(), tx, "alice", "legal_name", "Synthetic"))
	require.Equal(t, 1, tx.calls)

	tx = &profileWriteTx{err: context.Canceled}
	err = broadcastprofile.Language(t.Context(), tx, "alice", "en")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
}
