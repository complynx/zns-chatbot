package task

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func startCommands(ru bool) []telegram.BotCommand {
	entries := [][3]string{
		{"start", "Start and help", "Начать и помощь"},
		{"menu", "Services and menu", "Сервисы и меню"},
		{"profile", "My profile", "Мой профиль"},
		{"name", "Change my name", "Изменить имя"},
		{"registrations", "My registrations and passes", "Мои регистрации и пассы"},
		{"register", "Register for an event", "Регистрация на эвент"},
		{"orders", "Orders", "Заказы"},
		{"massage", "Massage bookings", "Записи на массаж"},
		{"avatar", "My avatar", "Моя аватарка"},
		{"pairs", "Pair invitations", "Приглашения в пару"},
		{"payments", "Pass payment", "Оплата пасса"},
		{"knowledge", "Community and event knowledge", "База знаний сообщества и эвентов"},
	}
	commands := make([]telegram.BotCommand, 0, len(entries))
	for _, entry := range entries {
		commands = append(commands, telegram.BotCommand{
			Command: entry[0], Description: structuredText(ru, entry[1], entry[2]),
		})
	}
	return commands
}

func (s *Service) refreshStartMenu(ctx context.Context, chatID int64) error {
	// The default list is English; the Russian override is scoped to the same chat.
	if err := s.tg.SetChatCommands(ctx, chatID, "", startCommands(false)); err != nil {
		return err
	}
	if err := s.tg.SetChatCommands(ctx, chatID, "ru", startCommands(true)); err != nil {
		return err
	}
	return s.tg.SetChatMenuCommands(ctx, chatID)
}

// completedStartWelcome defers first-start guidance until the confirmed name is saved.
// Its outbox entries are committed with the completed run result across restart.
func (s *Service) completedStartWelcome(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
) ([]db.Outbound, error) {
	if i.Kind != kindMessage || (!c.WelcomeAfterOnboarding && strings.TrimSpace(i.Text) != startCommand) {
		return nil, nil
	}
	bot, organization, _ := s.iam.DeploymentScope()
	named, err := s.store.HasTelegramProfileName(ctx, bot, i.User.ID, organization)
	if err != nil || !named {
		return nil, err
	}
	rows, err := s.store.ListOutbox(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	prefix := t.ID.String() + ":start:welcome:"
	for _, row := range rows {
		if strings.HasPrefix(row.DedupKey, prefix) {
			return nil, nil
		}
	}
	// Chat settings are deterministic and can be reapplied if the following save fails.
	if err = s.refreshStartMenu(ctx, i.ChatID); err != nil {
		return nil, err
	}
	return s.startWelcomeOutput(ctx, i)
}
func (s *Service) startWelcomeOutput(ctx context.Context, i intent) ([]db.Outbound, error) {
	ru := s.languageFor(ctx, i) == "ru"
	text, _, err := s.structuredView(ctx, db.ProfileScope{}, startCommand, ru)
	if err != nil {
		return nil, err
	}
	text += structuredText(ru,
		"\n\nWrite what you need, or choose a command from /menu.",
		"\n\nНапиши, что тебе нужно, или выбери команду из /menu.")
	out := structuredOutput(i, text, nil, ru, "")
	for n := range out {
		out[n].DedupKey = "start:welcome:" + strconv.Itoa(n)
	}
	if s.options.MiniAppURL == "" {
		return out, nil
	}
	last := &out[len(out)-1]
	var payload telegram.SendMessageRequest
	if err = json.Unmarshal(last.Payload, &payload); err != nil {
		return nil, err
	}
	payload.ReplyMarkup = &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{{
			Text:   structuredText(ru, "Open Zinusya", "Открыть ЗиНуСю"),
			WebApp: &telegram.WebAppInfo{URL: s.options.MiniAppURL},
		}}},
	}
	last.Payload, err = json.Marshal(payload)
	return out, err
}
