package task

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestConversationalCaptionsRemainUnmodifiedSteering(t *testing.T) {
	t.Parallel()
	q := loom.Question{
		ID:        "q",
		ExpiresAt: time.Now().Add(time.Hour),
		Choices:   []loom.Choice{{ID: "one", Label: "Yes, but only for one person"}},
	}
	require.Equal(t, q.Choices[0].Label, questionKeyboard(q).Keyboard[0][0].Text)
	for _, text := range []string{"Yes", "No", "Cancel", "/cancel", q.Choices[0].Label} {
		update := telegram.Update{
			ID: 1,
			Message: &telegram.Message{
				From: &telegram.User{ID: 7},
				Chat: telegram.Chat{ID: 7, Type: "private"},
				Text: text,
			},
		}
		raw, err := json.Marshal(update)
		require.NoError(t, err)
		got, err := normalize(update, raw)
		require.NoError(t, err)
		require.Equal(t, kindMessage, got.Kind)
		input, err := controlInput(db.Task{}, got, loom.Run{Question: &q})
		require.NoError(t, err)
		require.Equal(t, "steering", input.Kind)
		require.Equal(t, text, input.Text)
		require.Empty(t, input.Decision)
	}
}

func TestReplyKeyboardIdentitySurvivesJSONBOrderWithoutRoundingNumbers(t *testing.T) {
	t.Parallel()
	original := loom.Approval{
		ID:        "approval",
		Arguments: json.RawMessage(`{"long_field":9007199254740993,"x":1,"fraction":1.2300e2}`),
	}
	stored := original
	stored.Arguments = json.RawMessage(`{ "x": 1.0, "fraction":123,"long_field": 9007199254740993 }`)
	require.Equal(t, keyboardPromptKey(original), keyboardPromptKey(stored))
	changed := original
	changed.Arguments = json.RawMessage(`{"long_field":9007199254740992,"x":1,"fraction":123}`)
	require.NotEqual(t, keyboardPromptKey(original), keyboardPromptKey(changed))
}

func TestRetiredPromptDistinguishesAnswersFromUnansweredExpiry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		prompt   string
		run      loom.Run
		answered bool
	}{
		{"answered question", "question:dialog-v1:q", loom.Run{Question: &loom.Question{Answer: &loom.InputRequest{Kind: "steering"}}}, true},
		{"approved action", "approval:dialog-v1:a", loom.Run{Approval: &loom.Approval{Decision: "approve"}}, true},
		{"rejected action", "approval:dialog-v1:a", loom.Run{Approval: &loom.Approval{Decision: "reject"}}, true},
		{"expired question", "question:dialog-v1:q", loom.Run{Question: &loom.Question{ExpiresAt: time.Now().Add(-time.Minute)}}, false},
		{"expired approval", "approval:dialog-v1:a", loom.Run{Approval: &loom.Approval{ExpiresAt: time.Now().Add(-time.Minute)}}, false},
		{"expired question after resolved approval", "question:dialog-v1:q", loom.Run{Question: &loom.Question{}, Approval: &loom.Approval{Decision: "approve"}}, false},
		{"expired approval after answered question", "approval:dialog-v1:a", loom.Run{Question: &loom.Question{Answer: &loom.InputRequest{}}, Approval: &loom.Approval{}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.answered {
				require.Equal(t, "Response received.", retiredKeyboardText(test.run, test.prompt, false))
				require.Equal(t, "Ответ получен.", retiredKeyboardText(test.run, test.prompt, true))
			} else {
				require.Equal(t, "This choice is no longer active.", retiredKeyboardText(test.run, test.prompt, false))
				require.Equal(t, "Этот выбор больше не актуален.", retiredKeyboardText(test.run, test.prompt, true))
			}
		})
	}
}
