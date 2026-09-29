package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConversationBudgetKeepsCurrentQuestion(t *testing.T) {
	t.Parallel()
	old, err := json.Marshal(strings.Repeat("older context ", 3000))
	require.NoError(t, err)
	in := Input{Language: "ru", Text: "What is available?", History: []Event{
		{Kind: "reply", Content: old}, {Kind: "reply", Content: old},
	}}
	data, err := conversationalInput(in)
	require.NoError(t, err)
	require.LessOrEqual(t, len(data), maxInputBytes)
	require.True(t, strings.HasSuffix(string(data), `"What is available?"`))
	require.NotContains(t, string(data), `"language"`)
	require.Equal(t, "ru", in.Language, "the UI preference remains intact outside provider input")
	require.Len(t, in.History, 2)
}

func TestConversationRejectsOversizedCurrentQuestion(t *testing.T) {
	t.Parallel()
	_, err := conversationalInput(Input{Text: strings.Repeat("x", maxInputBytes)})
	require.Error(t, err, "duplicating the current utterance must not bypass the request budget")
}
