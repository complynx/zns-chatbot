package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

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
			b := Bot{Scripts: scriptEvaluatorFunc(func(context.Context, scriptclient.Request) (json.RawMessage, error) {
				return scenario.result, scenario.err
			})}
			run, err := b.evaluateScript(ctx, agent.ScriptProposal{Code: "return null;", InputJSON: "null"})
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
