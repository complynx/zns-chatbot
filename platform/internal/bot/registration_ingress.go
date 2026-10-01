package bot

import (
	"context"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/registrationnative"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) saveRegistrationIngress(ctx context.Context, tx pgx.Tx, update telegram.Update) error {
	in, ok := parseUpdate(update)
	if !ok || b.Delivery.BotID <= 0 {
		return nil
	}
	var binding *registrationingress.NativeBinding
	if update.Callback != nil && strings.HasPrefix(in.text, passMenuPrefix) {
		envelope, found, err := registrationnative.Classify(
			ctx,
			tx,
			in.chat,
			strings.TrimPrefix(in.text, passMenuPrefix),
		)
		if err != nil {
			return err
		}
		if found {
			binding, err = envelope.Binding()
			if err != nil {
				return err
			}
		}
	}
	return registrationingress.SaveClassifiedTelegram(registrationingress.WithClock(ctx, b.RegistrationClock), tx,
		registrationingress.Reference{BotID: b.Delivery.BotID, UpdateID: update.ID}, in.chat, binding)
}

// Intake and direct Handle consumers share one durable reference. Missing bot
// identity is rejected at the registration boundary, not invented from a token.
func (b *Bot) registrationIngressContext(ctx context.Context, update telegram.Update) (context.Context, error) {
	if _, ok := parseUpdate(update); !ok || b.Delivery.BotID <= 0 {
		return ctx, nil
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return ctx, inboxDatabaseError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = b.saveRegistrationIngress(ctx, tx, update); err != nil {
		return ctx, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ctx, inboxDatabaseError(ctx, err)
	}
	return registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: b.Delivery.BotID, UpdateID: update.ID},
	), nil
}
