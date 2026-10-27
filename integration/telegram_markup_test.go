package integration_test

import "github.com/complynx/zns-chatbot/internal/telegram"

// Reject null arrays like Bot API instead of silently accepting an unsendable outbox.
func invalidTelegramMarkup(markup telegram.ReplyMarkup) bool {
	switch keyboard := markup.(type) {
	case *telegram.InlineKeyboardMarkup:
		if keyboard.InlineKeyboard == nil {
			return true
		}
	case *telegram.ReplyKeyboardMarkup:
		if keyboard.Keyboard == nil {
			return true
		}
	}
	return false
}
