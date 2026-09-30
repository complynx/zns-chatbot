package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func scriptLocalContextPool(t *testing.T, failure error) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	attempts := new(atomic.Int32)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		attempts.Add(1)
		return fmt.Errorf("private script connection detail: %w", failure)
	}
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db, attempts
}

func TestScriptSQLLocalContextFailurePreventsVMAdmission(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, attempts := scriptLocalContextPool(t, failure)
			calls := 0
			host := ScriptHost{Store: ScriptStore{DB: db}, Worker: scriptEvaluatorFunc(
				func(context.Context, scriptclient.Request) (json.RawMessage, error) {
					calls++
					return json.RawMessage(`1`), nil
				})}
			err := host.Perform(t.Context(), "alice", 1,
				agent.ScriptProposal{Code: "return 1;", InputJSON: "null"},
				&agent.Input{Script: &agent.ScriptContext{Remaining: 1}})
			require.Positive(t, attempts.Load(), "the actual ledger connection was attempted")
			require.NoError(t, t.Context().Err())
			require.Zero(t, calls, "no VM starts without durable admission")
			require.Equal(t, core.ErrDatabase, err)
		})
	}
}

func TestScriptSQLLocalContextFailureFencesCaughtCallbacks(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, attempts := scriptLocalContextPool(t, failure)
			catalog := &liveScriptCatalog{}
			executor := &catchingScriptExecutor{alternate: context.WithoutCancel(t.Context())}
			host := ScriptHost{
				Worker:   executor,
				Store:    ScriptStore{DB: db, StaleError: errors.New("retired source")},
				Registry: ScriptRegistry{Catalog: catalog},
			}
			_, err := host.evaluateTools(t.Context(), "alice", 1, 0,
				agent.ScriptProposal{Code: "return 1;", InputJSON: "null"}, &agent.Input{})
			require.Positive(t, attempts.Load(), "AdmitSource reached the actual ledger connection")
			require.NoError(t, t.Context().Err())
			require.Equal(t, core.ErrDatabase, err)
			require.ErrorIs(t, executor.stopped, core.ErrDatabase)
			require.Len(t, executor.calls, 2)
			for _, callErr := range executor.calls {
				require.Equal(t, core.ErrDatabase, callErr)
			}
			require.Equal(t, 2, catalog.checks, "the second callback cannot reach authority or effects")
		})
	}
}

func TestScriptAuthoritySQLLocalDeadlineRetainsOrigin(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		call func(context.Context, PlanAuthorization) error
	}{
		{"completed plan", func(ctx context.Context, policy PlanAuthorization) error {
			return policy.ValidatePlan(ctx, "alice", 1, interaction.SavedPlan{})
		}},
		{"missing reply", func(ctx context.Context, policy PlanAuthorization) error {
			return policy.validateMissingReply(ctx, "alice", 1)
		}},
		{"history interactions", func(ctx context.Context, policy PlanAuthorization) error {
			return policy.ValidateHistoryInteractions(ctx, "alice", 1, 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db, attempts := scriptLocalContextPool(t, context.DeadlineExceeded)
			// No downstream policy is installed: failed SQL must return before invoking it.
			err := test.call(t.Context(), PlanAuthorization{DB: db})
			require.Positive(t, attempts.Load())
			require.NoError(t, t.Context().Err())
			require.Equal(t, core.ErrDatabase, err)
		})
	}
}

func TestScriptSQLParentContextFailureRemainsCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, _ := scriptLocalContextPool(t, failure)
			ctx, cancel := context.WithDeadline(t.Context(), time.Time{})
			if errors.Is(failure, context.Canceled) {
				cancel()
				ctx, cancel = context.WithCancel(t.Context())
				cancel()
			}
			defer cancel()
			err := (PlanAuthorization{DB: db}).validateMissingReply(ctx, "alice", 1)
			require.ErrorIs(t, err, failure)
			require.False(t, core.IsDatabaseFailure(err))
		})
	}
}

func TestScriptProviderDeadlineAndInvalidJSONRemainNonSQL(t *testing.T) {
	t.Parallel()
	host := ScriptHost{Worker: scriptEvaluatorFunc(
		func(context.Context, scriptclient.Request) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		})}
	run, err := host.evaluate(t.Context(), agent.ScriptProposal{Code: "return 1;", InputJSON: "null"})
	require.NoError(t, err)
	require.Equal(t, scriptTimeout, run.Error)
	err = host.Perform(t.Context(), "alice", 1,
		agent.ScriptProposal{Code: "return 1;", InputJSON: "{"},
		&agent.Input{Script: &agent.ScriptContext{Remaining: 1}})
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err), "invalid proposal JSON never reaches SQL")
}
