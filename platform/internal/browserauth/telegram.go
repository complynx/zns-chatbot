package browserauth

import (
	"context"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func message(locale string, id i18n.ID, values map[string]string) string {
	text, _ := i18n.Translate(locale, id, values)
	return text
}

func (s *Service) sendConsent(ctx context.Context, recipient Recipient, id, origin string) error {
	_, err := s.TG.Send(ctx, telegram.Send{
		ChatID: recipient.TelegramID,
		Text: message(
			recipient.Language,
			i18n.BrowserAuthRequest,
			map[string]string{"origin": origin, "request": id[:8]},
		),
		Markup: telegram.Markup{Rows: [][]telegram.Button{{
			{Text: message(recipient.Language, i18n.BrowserAuthApprove, nil), Data: "ba|" + id + "|approve"},
			{Text: message(recipient.Language, i18n.BrowserAuthDecline, nil), Data: "ba|" + id + "|decline"},
		}}},
	})
	return err
}

// Decide receives only callbacks from the authenticated private Telegram intake.
// Atomic state transition makes duplicates and old buttons unable to approve a
// different or cancelled request. A failed edit cannot undo durable consent.
func (s *Service) Decide(ctx context.Context, callback telegram.Callback, locale string) error {
	parts := strings.Split(callback.Data, "|")
	if len(parts) != 3 || parts[0] != "ba" || len(parts[1]) != 32 ||
		(parts[2] != "approve" && parts[2] != "decline") || callback.From.IsBot ||
		callback.Message.Chat.Type != "private" || callback.Message.Chat.ID != callback.From.ID {
		return ErrIdentity
	}
	state := "declined"
	label := i18n.BrowserAuthDeclined
	if parts[2] == "approve" {
		state = stateApproved
		label = i18n.BrowserAuthApproved
	}
	result, err := s.DB.Exec(
		ctx,
		`UPDATE bot.browser_auth SET state=$3,session_expires_at=CASE WHEN $3='approved' THEN now()+interval '24 hours' ELSE NULL END WHERE id=$1 AND telegram_id=$2 AND state='pending' AND expires_at>now()`,
		parts[1],
		callback.From.ID,
		state,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		label = i18n.BrowserAuthStale
	}
	text := message(locale, label, nil)
	var answer bool
	if err = s.TG.Call(
		ctx,
		"answerCallbackQuery",
		map[string]any{"callback_query_id": callback.ID, "text": text},
		&answer,
	); err != nil {
		return err
	}
	return s.TG.Edit(
		ctx,
		telegram.Send{
			ChatID:    callback.From.ID,
			MessageID: callback.Message.ID,
			Text:      text,
			Markup:    telegram.Markup{Rows: [][]telegram.Button{}},
		},
	)
}
