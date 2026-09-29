package bot

import (
	"context"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Registration is an idempotent replacement on every exclusive poller startup.
// No legacy version marker can suppress a changed command catalog or bot token.
func (b *Bot) registerTelegramMenu(ctx context.Context) error {
	if err := telegram.RetryControl(ctx, b.TG.SetDefaultCommandsMenu); err != nil {
		return err
	}
	for _, locale := range []i18n.Locale{i18n.English, i18n.Russian} {
		commands, err := telegramCommands(locale)
		if err != nil {
			return err
		}
		language := string(locale)
		if locale == i18n.English {
			language = ""
		}
		if err = telegram.RetryControl(
			ctx,
			func(callCtx context.Context) error { return b.TG.SetMyCommands(callCtx, language, commands) },
		); err != nil {
			return err
		}
	}
	return nil
}

func telegramCommands(locale i18n.Locale) ([]telegram.BotCommand, error) {
	specs := []struct {
		name        string
		description i18n.ID
	}{
		{
			botFamilyPasses,
			i18n.MenuRegistrationDescription,
		},
		{botFamilyMassage, i18n.CommandMassage},
		{agent.OrdersView, i18n.CommandOrders},
	}
	commands := make([]telegram.BotCommand, 0, len(specs))
	for _, command := range specs {
		text, err := i18n.Translate(string(locale), command.description, nil)
		if err != nil {
			return nil, err
		}
		commands = append(commands, telegram.BotCommand{Command: command.name, Description: text})
	}
	return commands, nil
}

func (b *Bot) startTelegramPolling(ctx context.Context) (int64, error) {
	if err := b.registerTelegramMenu(ctx); err != nil {
		return 0, fmt.Errorf("register Telegram command menu: %w", err)
	}
	if _, err := b.DB.Exec(
		ctx,
		`INSERT INTO bot.cursors(name,value) VALUES('telegram',0) ON CONFLICT DO NOTHING;
INSERT INTO bot.cursors(name,value) SELECT 'telegram_received',value FROM bot.cursors WHERE name='telegram'
ON CONFLICT(name) DO UPDATE SET value=GREATEST(bot.cursors.value,EXCLUDED.value)`,
	); err != nil {
		return 0, err
	}
	var offset int64
	if err := b.DB.QueryRow(ctx, `SELECT value FROM bot.cursors WHERE name='telegram_received'`).
		Scan(&offset); err != nil {
		return 0, err
	}
	return offset, nil
}
