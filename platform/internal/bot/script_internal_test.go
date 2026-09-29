package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
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
			b := Bot{
				Scripts: scriptEvaluatorFunc(func(_ context.Context, r scriptclient.Request) (json.RawMessage, error) {
					assert.JSONEq(t, `{"values":[2,3]}`, string(r.Input))
					return scenario.result, scenario.failure
				}),
			}
			run, err := b.evaluateScript(
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
	b := Bot{Scripts: scriptEvaluatorFunc(func(context.Context, scriptclient.Request) (json.RawMessage, error) {
		cancel()
		return nil, context.Canceled
	})}
	_, err := b.evaluateScript(ctx, agent.ScriptProposal{Code: "return 1;", InputJSON: "null"})
	require.ErrorIs(t, err, context.Canceled)
}
