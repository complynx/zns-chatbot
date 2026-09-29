package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/config"
)

// Core workers run only in API/app mode. Each failed startup unwinds workers
// already started; normal shutdown uses the same reverse order.
func startProductMaintenance(
	ctx context.Context,
	db *pgxpool.Pool,
	logger *slog.Logger,
	cfg config.Config,
	services appservices.Services,
) (func(), error) {
	stopMaintenance, err := startMaintenance(ctx, services, logger, cfg.Orders.ReminderAfter)
	if err != nil {
		return nil, err
	}
	stopSources, err := startAssistantSources(ctx, db, logger, cfg)
	if err != nil {
		stopMaintenance()
		return nil, err
	}
	stopFood, err := startFoodMaintenance(ctx, services.LegacyFood, logger)
	if err != nil {
		stopSources()
		stopMaintenance()
		return nil, err
	}
	return func() { stopFood(); stopSources(); stopMaintenance() }, nil
}
