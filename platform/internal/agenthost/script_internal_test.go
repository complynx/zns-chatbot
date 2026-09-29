package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptEvaluatorFunc func(context.Context, scriptclient.Request) (json.RawMessage, error)

func (f scriptEvaluatorFunc) Evaluate(ctx context.Context, r scriptclient.Request) (json.RawMessage, error) {
	return f(ctx, r)
}

func TestScriptBoundaryOnlyExplicitInputAndBoundedOutput(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name    string
		result  json.RawMessage
		failure error
		want    string
	}{
		{name: "valid", result: json.RawMessage(`{"total":5}`)},
		{name: "oversize", result: json.RawMessage(`"` + strings.Repeat("x", agent.MaxScriptResultBytes) + `"`), want: scriptResultLimit},
		{name: "invalid", result: json.RawMessage("undefined"), want: scriptInvalidResult},
		{name: "deadline", failure: context.DeadlineExceeded, want: scriptTimeout},
		{name: "error", failure: errors.New("private transport detail"), want: "execution_failed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			b := ScriptHost{
				Worker: scriptEvaluatorFunc(func(_ context.Context, r scriptclient.Request) (json.RawMessage, error) {
					assert.JSONEq(t, `{"values":[2,3]}`, string(r.Input))
					return scenario.result, scenario.failure
				}),
			}
			run, err := b.evaluate(
				t.Context(),
				agent.ScriptProposal{Code: "return input.values;", InputJSON: `{"values":[2,3]}`},
			)
			require.NoError(t, err)
			assert.Equal(t, scenario.want, run.Error)
			if scenario.want != "" {
				assert.Empty(t, run.Result)
			}
		})
	}
}

func TestScriptCancellationNotConvertedIntoToolSuccess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	b := ScriptHost{Worker: scriptEvaluatorFunc(func(context.Context, scriptclient.Request) (json.RawMessage, error) {
		cancel()
		return nil, context.Canceled
	})}
	_, err := b.evaluate(ctx, agent.ScriptProposal{Code: "return 1;", InputJSON: "null"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestEvaluatorDiagnosticsRetainSafeFailureClassification(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name          string
		result        json.RawMessage
		err           error
		outcome, code string
	}{
		{name: "deadline", err: context.DeadlineExceeded, outcome: "timeout", code: "timeout"},
		{name: "invalid", result: json.RawMessage(`undefined`), outcome: "invalid", code: "invalid_input"},
		{name: "oversize", result: json.RawMessage(`"` + strings.Repeat("x", agent.MaxScriptResultBytes) + `"`), outcome: "limited", code: "result_limit"},
		{name: "transport", err: errors.New("private-secret-transport"), outcome: "interrupted", code: "unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			var buffer bytes.Buffer
			recorder, err := observability.NewAgentEvents(
				slog.New(slog.NewJSONHandler(&buffer, nil)),
				"00000000-0000-4000-8000-000000000001",
				1,
			)
			require.NoError(t, err)
			ctx := observability.WithAgentEvents(t.Context(), recorder)
			ctx, span := observability.StartAgentEvent(
				ctx,
				observability.AgentEvent{Phase: "script", Operation: "js.run"},
			)
			b := ScriptHost{
				Worker: scriptEvaluatorFunc(func(context.Context, scriptclient.Request) (json.RawMessage, error) {
					return scenario.result, scenario.err
				}),
			}
			run, err := b.evaluate(ctx, agent.ScriptProposal{Code: "return null;", InputJSON: "null"})
			require.NoError(t, err)
			recordScriptOutcome(span, run.Error)
			span.Finish(nil)
			lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
			var envelope struct {
				Event string `json:"agent_event"`
			}
			require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &envelope))
			var event observability.AgentRecord
			require.NoError(t, json.Unmarshal([]byte(envelope.Event), &event))
			assert.Equal(t, scenario.outcome, event.Outcome)
			assert.Equal(t, scenario.code, event.ErrorCode)
			assert.NotContains(t, buffer.String(), "private-secret")
		})
	}
}

func TestScriptWireLogs(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct{ name, body, outcome, code string }{
		{"malformed", `{"result":private-secret}`, "invalid", "invalid_input"},
		{"result_limit", `{"result":"` + strings.Repeat("x", 64<<10) + `"}`, "limited", "result_limit"},
		{"wire_limit", strings.Repeat("x", 65<<10), "limited", "result_limit"},
		{"worker_error", `{"error":"private-secret"}`, "interrupted", "unavailable"},
		{"execution", `{"error":"execution_failed"}`, "error", "execution_failed"},
		{"empty_list", `{"result":[]}`, "no_results", ""},
		{"list", `{"result":["private-secret"]}`, "ok", ""},
		{"timeout", `{"error":"timeout"}`, "timeout", "timeout"},
		{"canceled", `{"error":"canceled"}`, "canceled", "canceled"},
		{"invalid", `{"error":"invalid_result"}`, "invalid", "invalid_input"},
		{"request", `{"error":"invalid_request"}`, "invalid", "invalid_input"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			client := diagnosticWireClient(t, scenario.body)
			var log bytes.Buffer
			recorder, err := observability.NewAgentEvents(
				slog.New(slog.NewJSONHandler(&log, nil)),
				"00000000-0000-4000-8000-000000000001",
				1,
			)
			require.NoError(t, err)
			ctx, span := observability.StartAgentEvent(
				observability.WithAgentEvents(t.Context(), recorder),
				observability.AgentEvent{Phase: "script", Operation: "js.run"},
			)
			b := ScriptHost{Worker: client}
			run, err := b.evaluate(ctx, agent.ScriptProposal{Code: "return null", InputJSON: "null"})
			require.NoError(t, err)
			recordScriptResult(span, run)
			span.Finish(nil)
			require.NotContains(t, log.String(), "private-secret")
			lines := strings.Split(strings.TrimSpace(log.String()), "\n")
			var envelope struct {
				Event string `json:"agent_event"`
			}
			require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &envelope))
			var record observability.AgentRecord
			require.NoError(t, json.Unmarshal([]byte(envelope.Event), &record))
			require.Equal(t, scenario.outcome, record.Outcome)
			require.Equal(t, scenario.code, record.ErrorCode)
		})
	}
}

func diagnosticWireClient(t *testing.T, body string) *scriptclient.Client {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	client, err := scriptclient.New(socket)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func TestScriptSourceStopKeepsParentUsable(t *testing.T) {
	t.Parallel()
	stale := errors.New("stale source")
	host := ScriptHost{Store: ScriptStore{StaleError: stale}}
	child, stop := context.WithCancelCause(t.Context())
	defer stop(nil)
	toolContext := context.WithValue(child, scriptSourceStopKey{}, stop)
	host.stopStale(toolContext, errors.New("database unavailable"))
	require.NoError(t, child.Err())
	host.stopStale(toolContext, stale)
	require.ErrorIs(t, context.Cause(child), stale)
	require.NoError(t, t.Context().Err())
}

// The fixture varies live rights while a VM keeps its original name scope.
type liveScriptCatalog struct {
	allow   bool
	extra   bool
	failure error
	checks  int
}

func (c *liveScriptCatalog) Capabilities(context.Context, string) (core.BusinessCapabilities, error) {
	c.checks++
	return core.BusinessCapabilities{}, c.failure
}
func (c *liveScriptCatalog) Passes(context.Context, string) ([]ScriptToolEntry, error) {
	entries := []ScriptToolEntry{}
	if c.allow {
		entries = append(entries, ScriptToolEntry{Descriptor: scriptclient.Tool{Name: "passes.read"}})
	}
	if c.extra {
		entries = append(entries, ScriptToolEntry{Descriptor: scriptclient.Tool{Name: "passes.extra"}})
	}
	return entries, nil
}
func (c *liveScriptCatalog) Lineup() ScriptToolEntry {
	return ScriptToolEntry{Descriptor: scriptclient.Tool{Name: "lineup.query"}}
}
func (c *liveScriptCatalog) Workflow(bool) []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) ProfileMutations(bool) []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) Memory() []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) Profile() []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) Reads() []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) Domains() []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) Orders(core.BusinessCapabilities) []ScriptToolEntry { return nil }

func (c *liveScriptCatalog) Food(context.Context, string) ([]ScriptToolEntry, error) { return nil, nil }

func (c *liveScriptCatalog) Privileged(context.Context, string) ([]ScriptToolEntry, error) {
	return nil, nil
}

func (c *liveScriptCatalog) Broadcast(context.Context, string) ([]ScriptToolEntry, error) {
	return nil, nil
}

func (c *liveScriptCatalog) Massage(context.Context, string) ([]ScriptToolEntry, error) {
	return nil, nil
}

func (c *liveScriptCatalog) Models(context.Context, string) ([]ScriptToolEntry, error) {
	return nil, nil
}

func (c *liveScriptCatalog) Credits(context.Context, string) ([]ScriptToolEntry, error) {
	return nil, nil
}

func (c *liveScriptCatalog) Knowledge(context.Context, string) ([]ScriptToolEntry, error) {
	return nil, nil
}

func TestScriptRegistryRefreshesRightsWithinOriginalBindings(t *testing.T) {
	t.Parallel()
	catalog := &liveScriptCatalog{allow: true}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: catalog}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	catalog.allow = false
	catalog.extra = true
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.NotContains(t, string(list), "passes.read")
	require.NotContains(t, string(list), "passes.extra")
	require.Contains(t, string(list), "lineup.query")
	_, err = host.discover(
		ctx,
		"owner",
		scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"passes.read"}`)},
	)
	require.Error(t, err)
	ctx, span := observability.StartAgentEvent(ctx, observability.AgentEvent{Phase: "tool", Operation: "passes.read"})
	_, err = host.callObserved(
		ctx,
		"owner",
		1,
		0,
		scriptclient.ToolCall{Name: "passes.read", Arguments: json.RawMessage(`{}`)},
		&agent.Input{},
		span,
	)
	require.ErrorContains(t, err, "tool unavailable")
	require.Equal(t, 4, catalog.checks, "initial catalog, list, help and known-name dispatch each authenticate")
	catalog.allow = true
	help, err := host.discover(
		ctx,
		"owner",
		scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"passes.read"}`)},
	)
	require.NoError(t, err)
	require.Contains(t, string(help), "passes.read")
	_, err = host.discover(
		ctx,
		"owner",
		scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"passes.extra"}`)},
	)
	require.Error(t, err, "new names cannot enter an existing VM")
	next, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	require.Len(t, next, 3, "the next VM sees the new grant")
	catalog.failure = errors.New("identity unavailable")
	_, err = host.Registry.Available(ctx, "owner")
	require.Error(t, err)
}
