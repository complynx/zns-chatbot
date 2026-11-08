package telegram_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPrivateUserAcceptsUnknownCommandsButRejectsForeignEnvelopes(t *testing.T) {
	t.Parallel()
	message := func(user telegram.User, chat telegram.Chat) telegram.Update {
		return telegram.Update{ID: 1, Message: &telegram.Message{From: &user, Chat: chat}}
	}
	user := telegram.User{ID: 1}
	chat := telegram.Chat{ID: 1, Type: "private"}
	callback := telegram.Update{ID: 1, CallbackQuery: &telegram.CallbackQuery{
		From: user, Message: &telegram.Message{Chat: chat}, Data: "unknown-old-command",
	}}
	both := message(user, chat)
	both.CallbackQuery = callback.CallbackQuery
	negative := message(user, chat)
	negative.ID = -1
	for _, test := range []struct {
		name  string
		input telegram.Update
		valid bool
	}{
		{"message", message(user, chat), true},
		{"unknown callback", callback, true},
		{"foreign chat", message(user, telegram.Chat{ID: 2, Type: "private"}), false},
		{"group", message(user, telegram.Chat{ID: 1, Type: "group"}), false},
		{"bot", message(telegram.User{ID: 1, IsBot: true}, chat), false},
		{"oversized ID", message(telegram.User{ID: 1 << 52}, chat), false},
		{"two envelopes", both, false},
		{"negative update", negative, false},
		{"inline callback", telegram.Update{CallbackQuery: &telegram.CallbackQuery{From: user}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, valid := test.input.PrivateUser()
			require.Equal(t, test.valid, valid)
			if valid {
				require.Equal(t, user, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}
