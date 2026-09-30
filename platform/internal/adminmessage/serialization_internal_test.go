package adminmessage

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
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestSourceLockPreservesSerializationRetry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"serialization", &pgconn.PgError{Code: "40001", Message: "private SQL"}, true},
		{"other SQL", &pgconn.PgError{Code: "P0001", Message: "private SQL"}, false},
		{"transport", io.EOF, false},
		{"driver-local cancellation", context.Canceled, false},
		{"driver-local deadline", context.DeadlineExceeded, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := readsource.LockActors(t.Context(), serializationLockTx{err: test.err}, []string{"alice"}, nil)
			require.Error(t, err)
			require.NoError(t, t.Context().Err(), "the SQL caller remains live")
			require.Equal(t, test.retry, serializationConflict(err))
			require.NotContains(t, err.Error(), "private SQL")
			var driverError *pgconn.PgError
			require.NotErrorAs(t, err, &driverError)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.True(t, core.IsDatabaseFailure(err))
			require.NotErrorIs(t, err, context.Canceled)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestSourceLockPreservesRequestCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			var ctx context.Context
			var cancel context.CancelFunc
			if errors.Is(failure, context.Canceled) {
				ctx, cancel = context.WithCancel(t.Context())
				cancel()
			} else {
				ctx, cancel = context.WithDeadline(t.Context(), time.Time{})
			}
			defer cancel()
			require.ErrorIs(t, ctx.Err(), failure)
			err := readsource.LockActors(ctx, serializationLockTx{err: failure}, []string{"alice"}, nil)
			require.ErrorIs(t, err, failure)
			require.False(t, core.IsDatabaseFailure(err))
			require.NotErrorIs(t, err, core.ErrDatabase)
			require.False(t, serializationConflict(err))
		})
	}
}

type serializationLockTx struct {
	pgx.Tx

	err error
}

func (tx serializationLockTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, tx.err
}
