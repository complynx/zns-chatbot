package bot

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

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
			b := Bot{Scripts: client}
			run, err := b.evaluateScript(ctx, agent.ScriptProposal{Code: "return null", InputJSON: "null"})
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
