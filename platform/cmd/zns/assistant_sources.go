package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/assistantsource"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func startAssistantSources(
	ctx context.Context,
	db *pgxpool.Pool,
	logger *slog.Logger,
	cfg config.Config,
) (func(), error) {
	runner, err := assistantsource.New(cfg.AssistantSources, knowledge.Service{DB: db}, logger)
	if err != nil {
		return nil, err
	}
	return runner.Start(ctx)
}
