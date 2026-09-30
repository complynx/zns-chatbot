package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestIdentityLookupFailurePreservesOnlySafeProvenance(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{
		&pgconn.PgError{Code: "P0001", Message: "private SQL password"},
		errors.New("private connection details"),
	} {
		err := identityLookupFailure(t.Context(), failure)
		require.ErrorIs(t, err, core.ErrDatabase)
		require.EqualError(t, err, "identity lookup unavailable")
		require.NotErrorIs(t, err, failure)
	}
	for _, err := range []error{nil, context.Canceled, context.DeadlineExceeded} {
		require.Equal(t, err, identityLookupFailure(t.Context(), err))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := identityLookupFailure(ctx, errors.New("driver cancellation text"))
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
}
