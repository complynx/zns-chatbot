package scriptworker_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestExecuteOmittedToolArguments(t *testing.T) {
	t.Parallel()
	var calls []string
	result, err := scriptworker.Execute(t.Context(), scriptprotocol.ExecuteRequest{
		Code:  `return [tools.models.effective(), await tools.preferences.get()];`,
		Input: json.RawMessage(`null`),
		Tools: []scriptprotocol.Tool{{Name: "models.effective"}, {Name: "preferences.get"}},
	}, func(_ context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
		calls = append(calls, call.Name)
		require.JSONEq(t, `{}`, string(call.Arguments))
		return json.RawMessage(`{"ok":true}`), nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"models.effective", "preferences.get"}, calls)
	require.JSONEq(t, `[{"ok":true},{"ok":true}]`, string(result))
}

func TestExecuteExplicitInvalidArgumentsRemainInvalid(t *testing.T) {
	t.Parallel()
	for _, argument := range []string{"undefined", "null", "[]", "42", `"text"`} {
		t.Run(argument, func(t *testing.T) {
			t.Parallel()
			_, err := scriptworker.Execute(t.Context(), scriptprotocol.ExecuteRequest{
				Code: `return tools.models.effective(` + argument + `);`, Input: json.RawMessage(`null`),
				Tools: []scriptprotocol.Tool{{Name: "models.effective"}},
			}, func(context.Context, scriptprotocol.ToolCall) (json.RawMessage, error) {
				t.Error("invalid explicit argument reached host")
				return json.RawMessage(`{}`), nil
			})
			require.ErrorIs(t, err, scriptworker.ErrExecution)
		})
	}
}
