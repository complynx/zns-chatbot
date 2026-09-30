package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const privateSQLDetail = "private sql detail"

// Authentication succeeds for the initial catalog, then every later live check fails.
type failingCapabilityCatalog struct {
	*liveScriptCatalog

	failure error
}

func (c *failingCapabilityCatalog) Capabilities(context.Context, string) (core.BusinessCapabilities, error) {
	c.checks++
	if c.checks > 1 {
		return core.BusinessCapabilities{}, c.failure
	}
	return core.BusinessCapabilities{}, nil
}

// catchingScriptExecutor models script code that catches each tool error and
// continues. Callbacks receive a context unrelated to the run context.
type catchingScriptExecutor struct {
	alternate context.Context
	calls     []error
	stopped   error
}

func (*catchingScriptExecutor) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	return nil, errors.New("unused")
}

func (e *catchingScriptExecutor) Execute(
	ctx context.Context,
	_ scriptclient.Request,
	_ []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	for range 2 {
		_, err := callback(e.alternate, scriptclient.ToolCall{Name: "lineup.query", Arguments: json.RawMessage(`{}`)})
		e.calls = append(e.calls, err)
	}
	e.stopped = context.Cause(ctx)
	return json.RawMessage(`{"continued":true}`), nil
}

func runCatchingScript(t *testing.T, failure error) (*catchingScriptExecutor, *failingCapabilityCatalog,
	agent.ScriptRun, error) {
	t.Helper()
	catalog := &failingCapabilityCatalog{liveScriptCatalog: &liveScriptCatalog{}, failure: failure}
	executor := &catchingScriptExecutor{alternate: context.WithoutCancel(t.Context())}
	host := ScriptHost{
		Worker:   executor,
		Store:    ScriptStore{StaleError: errors.New("stale source")},
		Registry: ScriptRegistry{Catalog: catalog},
	}
	run, err := host.evaluateTools(t.Context(), "owner", 1, 0,
		agent.ScriptProposal{Code: "return 1;", InputJSON: "null"}, &agent.Input{})
	return executor, catalog, run, err
}

func TestScriptCaughtDatabaseToolFailureStopsLaterEffects(t *testing.T) {
	t.Parallel()
	for name, failure := range map[string]error{
		"marker":  core.ErrDatabase,
		"pg":      &pgconn.PgError{Code: "XX000", Message: privateSQLDetail},
		"wrapped": fmt.Errorf("orders: %w", core.DatabaseFailure(errors.New("orders unavailable"))),
		"joined":  errors.Join(errors.New("stale source"), &pgconn.PgError{Message: privateSQLDetail}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			executor, catalog, _, err := runCatchingScript(t, failure)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.EqualError(t, err, "database unavailable")
			require.ErrorIs(t, executor.stopped, core.ErrDatabase, "the worker run is cancelled with SQL provenance")
			require.Len(t, executor.calls, 2)
			for _, callErr := range executor.calls {
				require.EqualError(t, callErr, "database unavailable")
				require.NotContains(t, callErr.Error(), privateSQLDetail)
			}
			require.Equal(t, 2, catalog.checks, "the fenced second callback never reaches live authority")
		})
	}
}

func TestScriptNonDatabaseToolFailureKeepsCatchableBehavior(t *testing.T) {
	t.Parallel()
	for name, failure := range map[string]error{
		"eof":      io.EOF,
		"network":  &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")},
		"http500":  &core.ProblemError{Status: http.StatusInternalServerError, Code: "internal"},
		"domain":   &core.ProblemError{Status: http.StatusConflict, Code: "conflict"},
		"canceled": context.Canceled,
		"deadline": context.DeadlineExceeded,
		"stale":    errors.New("stale source"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			executor, catalog, run, err := runCatchingScript(t, failure)
			require.NoError(t, err)
			require.JSONEq(t, `{"continued":true}`, string(run.Result))
			require.NoError(t, executor.stopped)
			for _, callErr := range executor.calls {
				require.EqualError(t, callErr, "tool unavailable")
			}
			require.Equal(t, 3, catalog.checks, "a non-database failure may be caught and retried")
		})
	}
}

func TestScriptDatabaseStopOutranksStale(t *testing.T) {
	t.Parallel()
	stale := errors.New("stale source")
	host := ScriptHost{Store: ScriptStore{StaleError: stale}}
	joined := errors.Join(stale, &pgconn.PgError{Message: privateSQLDetail})
	require.Nil(t, ScriptSourceFailure(joined, stale), "a joined SQL failure is not a recoverable stale outcome")
	require.JSONEq(t, `{"error":"stale","restart":true}`, string(ScriptSourceFailure(stale, stale)))
	_, span := observability.StartAgentEvent(t.Context(), observability.AgentEvent{Phase: "tool", Operation: "x"})
	result, outcome, err := NormalizeScriptOutcome(nil, joined, span, stale, joined)
	require.True(t, core.IsDatabaseFailure(err))
	require.Nil(t, result)
	require.Empty(t, outcome)

	fenced := func() (context.Context, context.Context, *scriptDatabaseFence) {
		child, stop := context.WithCancelCause(t.Context())
		t.Cleanup(func() { stop(nil) })
		fence := &scriptDatabaseFence{}
		ctx := context.WithValue(context.WithValue(child, scriptSourceStopKey{}, stop), scriptDatabaseFenceKey{}, fence)
		return child, ctx, fence
	}
	child, ctx, fence := fenced()
	host.stopStale(ctx, joined)
	require.ErrorIs(t, context.Cause(child), core.ErrDatabase)
	require.EqualError(t, context.Cause(child), "database unavailable")
	require.True(t, fence.tripped(child))

	child, ctx, fence = fenced()
	host.stopStale(ctx, stale)
	require.False(t, fence.tripped(child))
	host.stopStale(ctx, core.ErrDatabase)
	require.ErrorIs(t, context.Cause(child), stale, "the first cancellation cause is unchanged")
	require.True(t, fence.tripped(child), "a later SQL failure still outranks the earlier stale stop")
	require.NoError(t, t.Context().Err())
}

func TestScriptRegistryPreservesOnlyDatabaseProvenance(t *testing.T) {
	t.Parallel()
	catalog := &liveScriptCatalog{failure: &pgconn.PgError{Message: privateSQLDetail}}
	registry := ScriptRegistry{Catalog: catalog}
	_, err := registry.Resolve(t.Context(), "owner", "lineup.query")
	require.EqualError(t, err, "database unavailable")
	_, err = registry.Available(t.Context(), "owner")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), privateSQLDetail)
	catalog.failure = &core.ProblemError{Status: http.StatusServiceUnavailable, Code: "unavailable"}
	_, err = registry.Resolve(t.Context(), "owner", "lineup.query")
	require.EqualError(t, err, "tool unavailable")
	require.NotErrorIs(t, err, core.ErrDatabase)
}
