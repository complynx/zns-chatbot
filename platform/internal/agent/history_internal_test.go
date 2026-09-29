package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func TestHistorySummaryUsesSemanticModelAndStrictResult(t *testing.T) {
	t.Parallel()
	input := HistorySummaryInput{
		Previous: "Earlier preferences",
		Events:   []conversation.Event{{Kind: "user", Text: "I prefer vegetarian meals"}},
	}
	summary, err := summarizeHistory(t.Context(), input, func(_ context.Context, p providerPrompt) (string, error) {
		assert.Contains(t, p.instructions, "real semantic summary")
		assert.Contains(t, string(p.input), "vegetarian")
		assert.NotContains(t, p.schema, "action")
		return `{"text":"The user prefers vegetarian food."}`, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "The user prefers vegetarian food.", summary)
	_, err = decodeHistorySummary([]byte(`{"text":"ok","text":"other"}`))
	require.Error(t, err)
	require.Error(
		t,
		Validate(
			Plan{
				View:          "workflow",
				HistoryAction: &HistoryProposal{},
				ScriptAction:  &ScriptProposal{Code: "return 1", InputJSON: "null"},
			},
		),
	)
}
