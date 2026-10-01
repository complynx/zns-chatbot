package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) renderBotIntent(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	if !i.Reference.Valid(i.Owner) {
		return botRenderedDelivery{}, botdelivery.ErrBinding
	}
	switch i.Reference.Kind {
	case botdelivery.IdentityIntent:
		text, err := i18n.Translate(i.Reference.Language, i18n.IdentityUnavailable, nil)
		return botRenderedDelivery{Payload: telegram.Send{ChatID: i.Chat, Text: text}}, err
	case botdelivery.CardIntent:
		return b.renderBotCard(ctx, i)
	case botdelivery.ResultIntent:
		return b.renderBotResult(ctx, i)
	case botdelivery.DocumentIntent:
		return b.renderBotDocument(ctx, i)
	default:
		return botRenderedDelivery{}, botdelivery.ErrBinding
	}
}

func (b *Bot) renderBotPassRedaction(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	saved, revision, err := b.passMenuRecord(ctx, i.Owner)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	if !saved.Redacted || revision != i.Reference.Revision {
		return botRenderedDelivery{}, botdelivery.ErrStale
	}
	pref, err := b.API.Preferences(ctx, i.Owner)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	text, err := i18n.Translate(pref.Language, i18n.RegistrationUnavailable, nil)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	payload := telegram.Send{ChatID: i.Chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	err = b.DB.QueryRow(ctx, "SELECT message_id FROM bot.pass_views WHERE owner=$1 AND revision=$2", i.Owner, revision).
		Scan(&payload.MessageID)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	if payload.MessageID <= 0 {
		return botRenderedDelivery{}, botdelivery.ErrStale
	}
	return botRenderedDelivery{
		Payload: payload,
		Receipt: botdelivery.Continuation{Kind: botFamilyPassRedaction, Revision: revision},
	}, nil
}

// Receipt retirement uses the durable actual target, never the current view row.
func (b *Bot) renderBotPassReceiptRedaction(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	if i.Phase != botPhaseEdit || i.Target <= 0 || i.Target != i.Reference.Version {
		return botRenderedDelivery{}, botdelivery.ErrBinding
	}
	if _, err := b.derivedReplyVisible(ctx, i.Owner, i.Reference.Revision); err != nil {
		return botRenderedDelivery{}, err
	}
	pref, err := b.API.Preferences(ctx, i.Owner)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	text, err := i18n.Translate(pref.Language, i18n.RegistrationUnavailable, nil)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	return botRenderedDelivery{Payload: telegram.Send{
		ChatID: i.Chat, MessageID: i.Target, Text: text,
		Markup: telegram.Markup{Rows: [][]telegram.Button{}},
	}}, nil
}
