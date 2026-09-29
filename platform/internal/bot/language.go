package bot

import (
	"context"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const languageCallbackPrefix = "language:"
const languageKey = "language"

func isLanguageAction(text string) bool {
	return text == "/language" || strings.HasPrefix(text, "/language ") ||
		strings.HasPrefix(text, languageCallbackPrefix)
}

func (b *Bot) handleLanguage(ctx context.Context, in incoming, update telegram.Update) error {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	selected := strings.TrimSpace(strings.TrimPrefix(in.text, "/language"))
	if update.Callback != nil {
		selected = strings.TrimPrefix(in.text, languageCallbackPrefix)
	}
	noticeID := i18n.LanguageChoose
	if selected != "" {
		if i18n.IsSupported(selected) {
			preference, err = b.API.SetLanguageWithOperation(
				ctx,
				in.owner,
				selected,
				false,
				core.LanguageOperationKey("tg-language-"+strconv.FormatInt(update.ID, 10)),
			)
			if err != nil {
				return err
			}
			noticeID = i18n.LanguageSaved
		} else {
			noticeID = i18n.LanguageInvalid
		}
	}
	text, err := i18n.Translate(preference.Language, noticeID, map[string]string{languageKey: preference.Language})
	if err != nil {
		return err
	}
	current, err := i18n.Translate(
		preference.Language,
		i18n.LanguageCurrent,
		map[string]string{languageKey: preference.Language},
	)
	if err != nil {
		return err
	}
	payload := telegram.Send{
		ChatID: in.chat,
		Text:   text + "\n" + current,
		Markup: telegram.Markup{Rows: [][]telegram.Button{}},
	}
	for _, locale := range i18n.SupportedLocales() {
		payload.Markup.Rows = append(
			payload.Markup.Rows,
			[]telegram.Button{{Text: string(locale), Data: languageCallbackPrefix + string(locale)}},
		)
	}
	if err = b.deliverOrderCard(ctx, in.owner, languageKey, payload); err != nil {
		return err
	}
	if err = b.record(ctx, in.owner, update.ID, languageKey, preference); err != nil {
		return err
	}
	if err = b.refreshLanguageViews(ctx, in); err != nil {
		return err
	}
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

func (b *Bot) refreshLanguageViews(ctx context.Context, in incoming) error {
	var hasOrders bool
	err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.order_cards WHERE owner=$1 AND card_key NOT IN ('language','profile'))`, in.owner).
		Scan(&hasOrders)
	if err != nil {
		return err
	}
	if hasOrders {
		if err = b.RenderOrders(ctx, in.owner, in.chat); err != nil {
			return err
		}
	}
	if err = b.refreshOpenProfile(ctx, in.owner, in.chat); err != nil {
		return err
	}
	if err = b.refreshOpenWorkflow(ctx, in.owner, in.chat); err != nil {
		return err
	}
	return nil
}
