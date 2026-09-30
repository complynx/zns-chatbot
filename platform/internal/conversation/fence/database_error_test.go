package fence_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type failingFenceTx struct {
	pgx.Tx

	failAt int
	calls  int
	err    error
}

func (tx *failingFenceTx) nextError() error {
	tx.calls++
	if tx.calls == tx.failAt {
		return tx.err
	}
	return nil
}

func (tx *failingFenceTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.nextError()
}

func (tx *failingFenceTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return generationRow{err: tx.nextError()}
}

type generationRow struct{ err error }

func (row generationRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if generation, ok := dest[0].(*int64); ok {
		*generation = 2
	}
	return nil
}

func TestFenceSQLOriginErrors(t *testing.T) {
	t.Parallel()
	generation := int64(2)
	cases := []struct {
		name  string
		calls int
		run   func(context.Context, pgx.Tx) error
	}{
		{"current", 1, func(ctx context.Context, tx pgx.Tx) error {
			_, err := fence.CurrentGeneration(ctx, tx, "actor")
			return err
		}},
		{
			"generation",
			3,
			func(ctx context.Context, tx pgx.Tx) error { return fence.LockGeneration(ctx, tx, "actor", &generation) },
		},
		{
			"owners",
			2,
			func(ctx context.Context, tx pgx.Tx) error { return fence.LockOwners(ctx, tx, []string{"actor"}) },
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for step := 1; step <= test.calls; step++ {
				for _, fault := range []error{io.EOF, context.Canceled, context.DeadlineExceeded, pgx.ErrNoRows} {
					tx := &failingFenceTx{failAt: step, err: fault}
					want := fault
					if errors.Is(fault, io.EOF) {
						want = core.ErrDatabase
					}
					require.ErrorIs(t, test.run(t.Context(), tx), want, "SQL step %d", step)
					require.Equal(t, step, tx.calls)
				}
			}
			require.NoError(t, test.run(t.Context(), &failingFenceTx{}))
		})
	}
}

func TestFenceDomainOutcomesSurvive(t *testing.T) {
	t.Parallel()
	require.NoError(t, fence.LockGeneration(t.Context(), nil, "actor", nil))
	require.NoError(t, fence.LockOwners(t.Context(), nil, nil))
	generation := int64(1)
	err := fence.LockGeneration(t.Context(), &failingFenceTx{}, "actor", &generation)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "history_stale", problem.Code)
	require.NotErrorIs(t, err, core.ErrDatabase)
}
