package readsource

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func actorParentContext(t *testing.T, failure error) context.Context {
	t.Helper()
	ctx := t.Context()
	if errors.Is(failure, context.Canceled) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	cancel()
	return expired
}

func TestMutationActorSQLCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, parentCancelled := range []bool{false, true} {
			name := failure.Error() + "/driver"
			if parentCancelled {
				name = failure.Error() + "/parent"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, want := t.Context(), core.ErrDatabase
				if parentCancelled {
					ctx, want = actorParentContext(t, failure), failure
				}
				for stage, query := range map[string]func() (pgx.Rows, error){
					"query":    rowsResult(nil, failure),
					"scan":     rowsResult(&faultRows{next: 1, scanErr: failure}, nil),
					"terminal": rowsResult(&faultRows{next: 1, err: failure}, nil),
				} {
					t.Run(stage, func(t *testing.T) {
						t.Parallel()
						tx := &faultTx{t: t, query: query}
						err := LockActors(ctx, tx, []string{"owner"}, nil)
						require.Equal(t, want, err)
						require.Equal(t, !parentCancelled, core.IsDatabaseFailure(err))
						require.Equal(t, 1, tx.queries)
					})
				}
			})
		}
	}
}
