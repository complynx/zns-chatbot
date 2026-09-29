package agent_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// TestLiveQuestionLanguage uses only synthetic conversations and existing CLI
// authentication. It is opt-in because each case calls the real model.
func TestLiveQuestionLanguage(t *testing.T) {
	t.Parallel()
	executable := os.Getenv("ZNS_LIVE_CODEX_EXECUTABLE")
	if executable == "" {
		t.Skip("ZNS_LIVE_CODEX_EXECUTABLE enables real-model language acceptance")
	}
	model := agent.Codex{Executable: executable, SyntheticOnly: true}
	for _, tc := range []struct {
		preference, previous, question, word string
	}{
		{"ru", "Ранее мы говорили по-русски.", "How many legs does a typical cat have? Reply only with the number written as a word.", "four"},
		{"en", "We previously spoke English.", "Сколько лап у обычной кошки? Ответь только числом, записанным словом.", "четыре"},
		{"ru", "Ранее мы говорили по-русски.", "Ile łap ma zwykły kot? Odpowiedz tylko liczbą zapisaną słownie.", "cztery"},
		{"en", "We previously spoke English.", "Wie viele Beine hat eine gewöhnliche Katze? Antworte nur mit der Zahl als Wort.", "vier"},
	} {
		previous, err := json.Marshal(tc.previous)
		require.NoError(t, err)
		plan, err := model.Plan(t.Context(), agent.Input{
			Language: tc.preference, Text: tc.question,
			History: []agent.Event{{Kind: "reply", Content: previous}},
		})
		require.NoError(t, err, "question: %s", tc.question)
		t.Logf("question=%q preference=%s reply=%q", tc.question, tc.preference, plan.Text)
		words := strings.FieldsFunc(strings.ToLower(plan.Text), func(r rune) bool { return !unicode.IsLetter(r) })
		require.Equal(t, []string{tc.word}, words, "reject mixed-language explanations and quoted answer words")
		require.Nil(t, plan.Action)
		require.Nil(t, plan.OrderAction)
		require.Nil(t, plan.ProfileAction)
		require.Nil(t, plan.MediaAction)
	}
}
