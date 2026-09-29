package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

const orderCodeParameter = "code"
const orderExtraParameter = "extra"
const orderCountryParameter = "country"

// Keep translation failures visible at the renderer boundary while composing a card.
type orderMessages struct {
	language string
	err      error
}

func (m *orderMessages) text(id i18n.ID, values map[string]string) string {
	if m.err != nil {
		return ""
	}
	var text string
	text, m.err = i18n.Translate(m.language, id, values)
	return text
}

func (m *orderMessages) number(value string) string {
	if m.err != nil {
		return ""
	}
	var text string
	text, m.err = i18n.FormatNumber(m.language, value)
	return text
}

func (m *orderMessages) mealDays(count int) string {
	if m.err != nil {
		return ""
	}
	var text string
	text, m.err = i18n.TranslateCount(m.language, i18n.MealDays, strconv.Itoa(count))
	return text
}

func (b *Bot) orderMessage(ctx context.Context, owner string, id i18n.ID, values map[string]string) (string, error) {
	prefs, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return "", err
	}
	return i18n.Translate(prefs.Language, id, values)
}

func (b *Bot) rememberOrderLocale(ctx context.Context, owner string, update int64) error {
	prefs, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	return b.record(ctx, owner, update, "orders_reply_locale", prefs.Language)
}

func (m *orderMessages) extra(key string) string {
	ids := map[string]i18n.ID{"preparty": i18n.OrderPreparty, "shuttle": i18n.OrderShuttle,
		"excursion_minsk": i18n.OrderMinsk, "excursion_grodno_overview": i18n.OrderGrodnoOverview,
		"excursion_grodno_gorodnitsa": i18n.OrderGrodnoGorodnitsa}
	if id, ok := ids[key]; ok {
		return m.text(id, nil)
	}
	return key
}
