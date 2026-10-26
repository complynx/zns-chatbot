package task

import (
	"context"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
)

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
	if i.StartContinuation != "" {
		return nil, nil
	}
	deferred, err := s.store.HasDeferredStartForTask(ctx, t.ID)
	if err != nil || deferred {
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
	s.refreshMenuQuietly(ctx, i.ChatID)
	return s.startWelcomeOutput(ctx, i)
}
func (s *Service) startWelcomeOutput(ctx context.Context, i intent) ([]db.Outbound, error) {
	ru := s.languageFor(ctx, i) == "ru"
	bot, org, _ := s.iam.DeploymentScope()
	owner, err := s.store.MenuRecipientByTelegram(ctx, bot, org, i.User.ID)
	if err != nil {
		return nil, err
	}
	text, err := s.menuText(
		ctx,
		db.ProfileScope{BotID: bot, Organization: org, Subject: owner.Subject, UserID: owner.ID},
		ru,
	)
	if err != nil {
		return nil, err
	}
	text += structuredText(ru,
		"\n\nWrite what you need, or choose a command from /menu.",
		"\n\nНапиши, что тебе нужно, или выбери команду из /menu.")
	i.StructuredCommand = startCommand
	out := structuredMenuOutput(i, text, ru, s.options.MiniAppURL)
	for n := range out {
		out[n].DedupKey = "start:welcome:" + strconv.Itoa(n)
	}
	return out, nil
}

// Telegram needs separate messages to remove a reply keyboard and show an inline entry.
// Reuse the welcome and command text; the normal outbox preserves their delivery order.
func structuredMenuOutput(i intent, text string, ru bool, formURL string) []db.Outbound {
	if formURL == "" {
		return structuredOutput(i, text, nil, ru, "")
	}
	header := structuredText(ru, structuredWelcomeLeadEN, structuredWelcomeLeadRU)
	out := structuredOutput(i, strings.TrimSpace(header), nil, ru, "")
	commands := strings.TrimSpace(strings.TrimPrefix(text, header))
	entry := structuredOutput(i, commands, nil, ru, formURL)
	for n := range entry {
		entry[n].DedupKey = "result:structured:" + strconv.Itoa(len(out)+n)
	}
	return append(out, entry...)
}
