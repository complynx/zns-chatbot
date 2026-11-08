package task

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

// Rejected intake has no task or model run. Telegram feedback is static and best effort.
func (s *Service) replyToBannedUser(parent context.Context, update telegram.Update, publicReason string) {
	user, valid := update.PrivateUser()
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	text := "You cannot perform this action."
	if languageChoice(user.LanguageCode, "en") == "ru" {
		text = "Вы не можете выполнить это действие."
	}
	if publicReason != "" {
		text += "\n\n" + publicReason
	}
	// Plain text prevents operator-supplied markup from becoming Telegram entities.
	if _, err := s.tg.SendMessage(ctx, telegram.SendMessageRequest{ChatID: user.ID, Text: text}); err != nil {
		s.record(parent, "ban_reply", "send_failed")
	}
}
