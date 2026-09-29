package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func runAPI(
	ctx context.Context,
	db *pgxpool.Pool,
	signer identity.Signer,
	logger *slog.Logger,
	cfg config.Config,
	runtime *observability.Runtime,
) error {
	verify, identityAdapter, identityLinks, err := runtimeAuth(db, cfg, signer)
	if err != nil {
		return err
	}
	deliverySettings, err := cfg.DeliverySettings()
	if err != nil {
		return err
	}
	model, err := botModel(cfg, runtime, credits.Service{DB: db, Enforce: cfg.Credits.Enforce})
	if err != nil {
		return err
	}
	tg, err := configuredTelegramClient(cfg, runtime, db, deliverySettings)
	if err != nil {
		return err
	}
	services := appservices.NewServices(db, appservices.Options{
		RegistrationRetention: cfg.Registration.Retention,
		NativeRegistrationAuthorizer: nativeRegistrationAuthorizer(
			db,
			deliverySettings.BotID,
			verify,
			identityAdapter,
			identityLinks,
			signer,
		),
		LegacyOrderBotID:     deliverySettings.BotID,
		InformalName:         broadcastOptions(model).InformalName,
		Delivery:             deliverySettings,
		AnnouncementBindings: &destination.Bindings{},
		DestinationResolver:  tg,
	})
	stop, err := startProductMaintenance(ctx, db, logger, cfg, services)
	if err != nil {
		return err
	}
	defer stop()
	handler, closeProvisioning, err := configureCoreProvisioning(ctx,
		api.AuthenticatedHandler(
			services,
			signer,
			logger,
			verify,
		),
		db, signer, cfg,
	)
	if err != nil {
		return err
	}
	defer closeProvisioning()
	return serve(ctx, telemetryHandler(runtime, handler), logger, cfg)
}

func startMaintenance(
	ctx context.Context,
	services appservices.Services,
	logger *slog.Logger,
	after time.Duration,
) (func(), error) {
	service := services.Orders
	attachments := services.Media
	registrations := services.Registration
	refreshAnnouncementBindings(ctx, services, logger)
	if _, err := registrations.ProcessPassportReminders(ctx); err != nil {
		return nil, err
	}
	if _, err := attachments.PruneExpired(ctx); err != nil {
		return nil, err
	}
	if _, err := service.QueueDueReminders(ctx, after); err != nil {
		return nil, err
	}
	if _, err := registrations.ProcessDeadlines(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		logger.WarnContext(ctx, "pass deadline scan pending", "error", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runMaintenanceTick(ctx, services, logger, after)
			}
		}
	}()
	return func() { cancel(); <-finished }, nil
}

func runMaintenanceTick(ctx context.Context, services appservices.Services, logger *slog.Logger, after time.Duration) {
	refreshAnnouncementBindings(ctx, services, logger)
	if _, err := services.Media.PruneExpired(ctx); err != nil {
		logger.WarnContext(ctx, "media cleanup pending")
	}
	if _, err := services.Orders.QueueDueReminders(ctx, after); err != nil {
		logger.WarnContext(ctx, "reminder scan pending", "error", err)
	}
	if _, err := services.Registration.ProcessDeadlines(ctx); err != nil {
		logger.WarnContext(ctx, "pass deadline scan pending", "error", err)
	}
}
