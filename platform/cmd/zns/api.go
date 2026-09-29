package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func runAPI(
	ctx context.Context,
	db *pgxpool.Pool,
	signer identity.Signer,
	logger *slog.Logger,
	cfg config.Config,
	runtime *observability.Runtime,
) error {
	verify, _, _, err := runtimeAuth(db, cfg, signer)
	if err != nil {
		return err
	}
	legacyBotID, err := cfg.LegacyOrderBotID()
	if err != nil {
		return err
	}
	stop, err := startProductMaintenance(ctx, db, logger, cfg, legacyBotID)
	if err != nil {
		return err
	}
	defer stop()
	model, err := botModel(cfg, runtime, credits.Service{DB: db, Enforce: cfg.Credits.Enforce})
	if err != nil {
		return err
	}
	handler, closeProvisioning, err := configureCoreProvisioning(ctx,
		api.AuthenticatedHandler(
			runtimeapp.NewServices(
				db,
				runtimeapp.Options{LegacyOrderBotID: legacyBotID, InformalName: broadcastOptions(model).InformalName},
			),
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

func startMaintenance(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, after time.Duration) (func(), error) {
	service := orders.Service{DB: db}
	attachments := media.Service{DB: db}
	registrations := passbooking.Service{DB: db}
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
				runMaintenanceTick(ctx, db, logger, after)
			}
		}
	}()
	return func() { cancel(); <-finished }, nil
}

func runMaintenanceTick(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, after time.Duration) {
	if _, err := (media.Service{DB: db}).PruneExpired(ctx); err != nil {
		logger.WarnContext(ctx, "media cleanup pending")
	}
	if _, err := (orders.Service{DB: db}).QueueDueReminders(ctx, after); err != nil {
		logger.WarnContext(ctx, "reminder scan pending", "error", err)
	}
	if _, err := (passbooking.Service{DB: db}).ProcessDeadlines(ctx); err != nil {
		logger.WarnContext(ctx, "pass deadline scan pending", "error", err)
	}
}
