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
) (runErr error) {
	verify, identityAdapter, identityLinks, err := observedRuntimeAuth(db, cfg, signer, runtime)
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fatal := newFatalLatch(cancel)
	stop, err := startProductMaintenance(ctx, db, logger, cfg, services, runtime, fatal.report)
	if err != nil {
		return fatal.result(err)
	}
	defer func() { stop(); runErr = fatal.result(runErr) }()
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
	onFatal func(error),
) (func(), error) {
	service := services.Orders
	attachments := services.Media
	registrations := services.Registration
	if err := refreshAnnouncementBindings(ctx, services, logger); err != nil {
		return nil, err
	}
	if _, err := registrations.ProcessPassportReminders(ctx); err != nil {
		return nil, err
	}
	if _, err := attachments.PruneExpired(ctx); err != nil {
		return nil, err
	}
	if _, err := service.QueueDueReminders(ctx, after); err != nil {
		return nil, err
	}
	if _, err := service.RouteRefunds(ctx); err != nil {
		return nil, err
	}
	if _, err := registrations.ProcessDeadlines(ctx); err != nil {
		if fatal := databaseFatal(err); fatal != nil {
			return nil, fatal
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		logger.WarnContext(ctx, "pass deadline scan pending", "error", err)
	}
	tick := func(live context.Context) error { return runMaintenanceTick(live, services, logger, after) }
	return startTicks(ctx, time.Minute, tick, onFatal), nil
}

// runMaintenanceTick returns only a safe positive database failure, stopping
// the pass there. Ordinary provider/domain failures are logged and retried on
// the next tick.
func runMaintenanceTick(
	ctx context.Context, services appservices.Services, logger *slog.Logger, after time.Duration,
) error {
	if err := refreshAnnouncementBindings(ctx, services, logger); err != nil {
		return err
	}
	if _, err := services.Media.PruneExpired(ctx); err != nil {
		if fatal := databaseFatal(err); fatal != nil {
			return fatal
		}
		logger.WarnContext(ctx, "media cleanup pending")
	}
	if _, err := services.Orders.QueueDueReminders(ctx, after); err != nil {
		if fatal := databaseFatal(err); fatal != nil {
			return fatal
		}
		logger.WarnContext(ctx, "reminder scan pending", "error", err)
	}
	if _, err := services.Orders.RouteRefunds(ctx); err != nil {
		if fatal := databaseFatal(err); fatal != nil {
			return fatal
		}
		logger.WarnContext(ctx, "refund routing pending", "error", err)
	}
	if _, err := services.Registration.ProcessDeadlines(ctx); err != nil {
		if fatal := databaseFatal(err); fatal != nil {
			return fatal
		}
		logger.WarnContext(ctx, "pass deadline scan pending", "error", err)
	}
	return nil
}
