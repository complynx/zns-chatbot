package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type startupAcceptor interface{ SetAccepting(bool) }

// HTTP listeners are serving, but readiness/intake stay closed until synchronization.
func readyApplication(ctx context.Context, c config.Config, intake startupAcceptor, logger *slog.Logger) error {
	if err := synchronizeWebhook(ctx, c); err != nil {
		return err
	}
	intake.SetAccepting(true)
	logger.InfoContext(ctx, "application ready")
	return nil
}

func synchronizeWebhook(ctx context.Context, c config.Config) error {
	if !c.Telegram.WebhookEnabled {
		return nil
	}
	client, err := telegram.New(c.Telegram.BaseURL, c.Telegram.Token, &http.Client{Timeout: c.HTTPTimeout})
	if err != nil {
		return err
	}
	setup, cancel := context.WithTimeout(ctx, c.HTTPTimeout)
	defer cancel()
	updates := []string{"message", "callback_query"}
	request := telegram.SetWebhookRequest{URL: c.TelegramWebhookURL(), SecretToken: c.Telegram.WebhookSecret,
		AllowedUpdates: updates, DropPendingUpdates: false}
	if err = client.SetWebhook(setup, request); err != nil {
		return fmt.Errorf("startup Telegram setWebhook: %w", err)
	}
	info, err := client.GetWebhookInfo(setup)
	if err != nil {
		return fmt.Errorf("startup Telegram getWebhookInfo: %w", err)
	}
	if info.URL != request.URL || len(info.AllowedUpdates) != len(updates) {
		return errors.New("startup Telegram webhook configuration mismatch")
	}
	for _, update := range updates {
		if !slices.Contains(info.AllowedUpdates, update) {
			return errors.New("startup Telegram webhook update types mismatch")
		}
	}
	return nil
}
