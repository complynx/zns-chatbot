package agenthost

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestRegistrationAdmissionsSQLPoolFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.DeadlineExceeded, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, attempts := scriptLocalContextPool(t, failure)
			got, err := (ScriptStore{DB: db}).ReadRegistrationOperations(t.Context(), "alice", "")
			require.Positive(t, attempts.Load())
			require.NoError(t, t.Context().Err())
			require.Nil(t, got)
			require.Equal(t, core.ErrDatabase, err)
		})
	}
}

func TestRegistrationAdmissionsSQLParentCancellation(t *testing.T) {
	t.Parallel()
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, _ := scriptLocalContextPool(t, io.EOF)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if deadline {
				ctx, cancel = context.WithDeadline(t.Context(), time.Time{})
				defer cancel()
			}
			got, err := (ScriptStore{DB: db}).ReadRegistrationOperations(ctx, "alice", "")
			require.Nil(t, got)
			require.ErrorIs(t, err, ctx.Err())
			require.False(t, core.IsDatabaseFailure(err))
		})
	}
}

func TestRegistrationAdmissionsMissingReferenceIsNotSQL(t *testing.T) {
	t.Parallel()
	db, attempts := scriptLocalContextPool(t, io.EOF)
	request, source, err := (ScriptStore{DB: db}).ReadRegistrationOperation(t.Context(), "alice", "")
	require.EqualError(t, err, "missing registration operation reference")
	require.False(t, core.IsDatabaseFailure(err))
	require.Nil(t, request)
	require.Nil(t, source)
	require.Zero(t, attempts.Load())
}
