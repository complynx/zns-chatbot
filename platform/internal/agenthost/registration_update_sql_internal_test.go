package agenthost

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestRegistrationUpdateSQLBoundary(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.DeadlineExceeded, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, attempts := scriptLocalContextPool(t, failure)
			got, err := (ScriptStore{DB: db}).ReadRegistrationOperationsForUpdate(t.Context(), "alice", 96)
			require.Positive(t, attempts.Load())
			require.NoError(t, t.Context().Err())
			require.Nil(t, got)
			require.Equal(t, core.ErrDatabase, err)
		})
	}
}

func TestRegistrationUpdateInvalidAndCancelled(t *testing.T) {
	t.Parallel()
	db, attempts := scriptLocalContextPool(t, io.EOF)
	store := ScriptStore{DB: db}
	for _, update := range []int64{0, -1} {
		got, err := store.ReadRegistrationOperationsForUpdate(t.Context(), "alice", update)
		require.Nil(t, got)
		require.EqualError(t, err, "invalid registration operation update")
		require.False(t, core.IsDatabaseFailure(err))
	}
	require.Zero(t, attempts.Load())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := store.ReadRegistrationOperationsForUpdate(ctx, "alice", 96)
	require.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
}
