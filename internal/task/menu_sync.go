package task

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const menuPageSize = 20
const menuRefreshInterval = 30 * time.Second
const menuPageInterval = 2 * time.Second
const menuImmediateTimeout = 2 * time.Second
const menuScanTimeout = 10 * time.Second

func startCommands(ru bool, a db.TelegramMenuAvailability, miniApp bool) []telegram.BotCommand {
	entries := []struct {
		command, en, ru string
		available       bool
	}{
		{"menu", "Services and menu", "Сервисы и меню", true},
		{"profile", "My profile", "Мой профиль", true},
		{"name", "Change my name", "Изменить имя", true},
		{"registrations", "My registrations and passes", "Мои регистрации и пассы", a.Registrations},
		{"register", "Register for an event", "Регистрация на эвент", a.Register},
		{"orders", "Orders", "Заказы", miniApp && a.Orders},
		{"massage", "Massage bookings", "Записи на массаж", miniApp && a.Massage},
		{"avatar", "My avatar", "Моя аватарка", miniApp},
		{"pairs", "Pair invitations", "Приглашения в пару", miniApp && a.Pairs},
		{"payments", "Pass payment", "Оплата пасса", miniApp && a.Payments},
	}
	commands := make([]telegram.BotCommand, 0, len(entries))
	for _, e := range entries {
		if e.available {
			commands = append(commands, telegram.BotCommand{Command: e.command, Description: structuredText(ru, e.en, e.ru)})
		}
	}
	return commands
}

func (s *Service) menuCommands(ctx context.Context, scope db.ProfileScope, ru bool) ([]telegram.BotCommand, error) {
	a, err := s.store.MenuAvailability(ctx, scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return []telegram.BotCommand{}, nil
	}
	if err != nil {
		return nil, err
	}
	return startCommands(ru, a, s.options.MiniAppURL != ""), nil
}

func (s *Service) menuText(ctx context.Context, scope db.ProfileScope, ru bool) (string, error) {
	commands, err := s.menuCommands(ctx, scope, ru)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	text.WriteString(structuredText(ru, structuredWelcomeEN, structuredWelcomeRU))
	for _, command := range commands {
		text.WriteString("\n/" + command.Command + " — " + command.Description)
	}
	return text.String(), nil
}

func (s *Service) refreshStartMenu(ctx context.Context, chatID int64) error {
	bot, org, _ := s.iam.DeploymentScope()
	owner, err := s.store.MenuRecipientByTelegram(ctx, bot, org, chatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.applyMenu(ctx, chatID, nil, nil)
	}
	if err != nil {
		return err
	}
	return s.refreshOwnerMenu(ctx, owner)
}

func (s *Service) refreshOwnerMenu(ctx context.Context, owner db.User) error {
	scope := db.ProfileScope{
		BotID:        owner.BotID,
		Organization: owner.Organization,
		Subject:      owner.Subject,
		UserID:       owner.ID,
	}
	a, err := s.store.MenuAvailability(ctx, scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.applyMenu(ctx, owner.TelegramUserID, nil, nil)
	}
	if err != nil {
		return err
	}
	en := startCommands(false, a, s.options.MiniAppURL != "")
	ru := startCommands(true, a, s.options.MiniAppURL != "")
	return s.applyMenu(ctx, owner.TelegramUserID, en, ru)
}

// Cache only complete successful applications. A partial or unknown outcome retries
// the same settings, while ordinary scans make no Telegram calls for unchanged menus.
func (s *Service) applyMenu(ctx context.Context, chatID int64, en, ru []telegram.BotCommand) error {
	if !s.menuMu.TryLock() {
		return nil
	}
	defer s.menuMu.Unlock()
	var key strings.Builder
	for _, commands := range [][]telegram.BotCommand{en, ru} {
		for _, c := range commands {
			key.WriteString(c.Command)
			key.WriteByte(0)
			key.WriteString(c.Description)
			key.WriteByte(0)
		}
		key.WriteByte(1)
	}
	if previous, ok := s.menuApplied[chatID]; ok && previous == key.String() {
		return nil
	}
	if err := s.tg.SetChatCommands(ctx, chatID, "", en); err != nil {
		return err
	}
	if err := s.tg.SetChatCommands(ctx, chatID, "ru", ru); err != nil {
		return err
	}
	if err := s.tg.SetChatMenuCommands(ctx, chatID); err != nil {
		return err
	}
	if s.menuApplied == nil {
		s.menuApplied = make(map[int64]string)
	}
	s.menuApplied[chatID] = key.String()
	return nil
}

func (s *Service) refreshMenuQuietly(ctx context.Context, chatID int64) {
	ctx, cancel := context.WithTimeout(ctx, menuImmediateTimeout)
	defer cancel()
	if err := s.refreshStartMenu(ctx, chatID); err != nil && ctx.Err() == nil {
		s.logger.WarnContext(ctx, "Telegram menu refresh unavailable", "class", "telegram_menu")
	}
}

func (s *Service) clearFallbackMenus(ctx context.Context) bool {
	cleared := true
	for _, scope := range []string{"default", "all_private_chats"} {
		for _, language := range []string{"", "ru"} {
			if err := s.tg.ClearDefaultCommands(ctx, scope, language); err != nil {
				cleared = false
			}
		}
	}
	return cleared
}

func (s *Service) scanMenuPage(ctx context.Context, after uuid.UUID) uuid.UUID {
	bot, org, _ := s.iam.DeploymentScope()
	owners, err := s.store.MenuRecipients(ctx, bot, org, after, menuPageSize)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "Telegram menu scan unavailable", "class", "telegram_menu")
		}
		return after
	}
	for _, owner := range owners {
		if err = s.refreshOwnerMenu(ctx, owner); err != nil && ctx.Err() == nil {
			s.logger.WarnContext(ctx, "Telegram menu refresh unavailable", "class", "telegram_menu")
		}
		after = owner.ID
	}
	if len(owners) < menuPageSize {
		return uuid.Nil
	}
	return after
}

func (s *Service) watchMenus(ctx context.Context, done chan struct{}) {
	defer close(done)
	var after uuid.UUID
	defaultsCleared := false
	delay := time.Duration(0)
	for {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, menuScanTimeout)
		if !defaultsCleared {
			defaultsCleared = s.clearFallbackMenus(attempt)
		}
		after = s.scanMenuPage(attempt, after)
		cancel()
		delay = menuPageInterval
		if after == uuid.Nil {
			delay = menuRefreshInterval
		}
	}
}
