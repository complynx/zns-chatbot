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

// prepareStart presents native guidance before the existing missing-name agent
// dialogue. The outbox receipt, not a local flag, proves that guidance was sent.
func (s *Service) prepareStart(ctx context.Context, t *db.Task, c checkpoint, i intent) (bool, error) {
	if i.Kind != kindMessage || strings.TrimSpace(i.Text) != startCommand || t.RunID != "" || c.Create != nil {
		return true, nil
	}
	rows, err := s.store.ListOutbox(ctx, t.ID)
	if err != nil {
		return false, s.wait()
	}
	prefix := t.ID.String() + ":start:welcome:"
	exists := false
	for _, row := range rows {
		if strings.HasPrefix(row.DedupKey, prefix) {
			exists = true
		}
	}
	if !exists {
		if err = s.queueStartWelcome(ctx, t, c, i); err != nil {
			return false, s.wait()
		}
	}
	if err = s.deliver(ctx, t, c); err != nil {
		return false, err
	}
	return s.startWelcomeReady(ctx, t, c, prefix)
}

func (s *Service) startWelcomeReady(ctx context.Context, t *db.Task, c checkpoint, prefix string) (bool, error) {
	rows, err := s.store.ListOutbox(ctx, t.ID)
	if err != nil {
		return false, s.wait()
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.DedupKey, prefix) || row.State == deliveryDelivered {
			continue
		}
		if t.RunID != "" || (row.State != stateDeliveryUnknown && row.State != stateDeliveryFailed) {
			return false, s.wait()
		}
		// No agent run exists yet. Fence this start task rather than signal
		// processing while its greeting needs manual reconciliation.
		t.State = row.State
		if err = s.save(ctx, t, c, nil); err != nil {
			return false, s.wait()
		}
		return false, nil
	}
	return true, nil
}

func (s *Service) queueStartWelcome(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	// These deterministic chat settings can be reapplied after an unknown response.
	if err := s.refreshStartMenu(ctx, i.ChatID); err != nil {
		return err
	}
	out, err := s.startWelcomeOutput(ctx, i)
	if err != nil {
		return err
	}
	return s.save(ctx, t, c, out)
}

func (s *Service) startWelcomeOutput(ctx context.Context, i intent) ([]db.Outbound, error) {
	ru := s.languageFor(ctx, i) == "ru"
	text, _, err := s.structuredView(ctx, db.ProfileScope{}, startCommand, ru)
	if err != nil {
		return nil, err
	}
	text += structuredText(ru,
		"\n\nWrite what you need, or choose a command from /menu. I’ll ask for your name if needed.",
		"\n\nНапиши, что тебе нужно, или выбери команду из /menu. Если понадобится, я попрошу назвать имя и фамилию.")
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
