package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const announcementBindingTTL = 5 * time.Minute
const announcementRefreshTimeout = 20 * time.Second

func configuredTelegramClient(
	cfg config.Config, runtime *observability.Runtime, db *pgxpool.Pool, settings delivery.Settings,
) (telegram.Client, error) {
	control, err := delivery.NewControlPacer(db, settings)
	if err != nil {
		return telegram.Client{}, err
	}
	return telegram.Client{
		Base:    cfg.Telegram.BaseURL,
		Token:   cfg.Telegram.Token.Value(),
		HTTP:    telemetryClient(runtime, "api", telegramClientTimeout),
		Control: control,
	}, nil
}

// Refresh runs in the existing joined maintenance worker, outside owner
// transactions. One unavailable alias does not discard other valid bindings.
func refreshAnnouncementBindings(ctx context.Context, services appservices.Services, logger *slog.Logger) {
	if services.Registration.AnnouncementBindings == nil {
		return
	}
	refreshCtx, cancel := context.WithTimeout(ctx, announcementRefreshTimeout)
	defer cancel()
	if err := services.Registration.RefreshAnnouncementDestinations(
		refreshCtx, services.AdminMessages.DestinationResolver, announcementBindingTTL,
	); err != nil && ctx.Err() == nil {
		logger.WarnContext(ctx, "announcement destination refresh incomplete", "error", err)
	}
}
