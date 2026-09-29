package main

import (
	"context"
	"errors"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const productionEnvironment = "production"
const openAIProvider = "openai"

func commandRole(command string) (runtimeapp.Role, bool) {
	switch command {
	case "api":
		return runtimeapp.API, true
	case "bot":
		return runtimeapp.Bot, true
	case "app":
		return runtimeapp.App, true
	default:
		return 0, false
	}
}

func closeRuntime(ctx context.Context, cfg config.Config, runtime *observability.Runtime, db *pgxpool.Pool) error {
	err := flushTelemetry(ctx, runtime, cfg.Shutdown.TelemetryFlush)
	if db != nil {
		db.Close()
	}
	return err
}

// openRuntimeDatabase applies deployment identity before the pool's initial ping.
func openRuntimeDatabase(
	ctx context.Context,
	cfg config.Config,
	runtime *observability.Runtime,
) (*pgxpool.Pool, error) {
	if _, managed := commandRole(os.Args[1]); !managed {
		return store.Open(ctx, cfg.Database.URL.Value(), runtime.PGXTracer())
	}
	instance, err := runtimeapp.EnvironmentInstance(cfg.Env == productionEnvironment)
	if err != nil {
		return nil, err
	}
	if instance == (runtimeapp.Instance{}) {
		return store.Open(ctx, cfg.Database.URL.Value(), runtime.PGXTracer())
	}
	if cfg.Model.Provider != openAIProvider || cfg.Media.URL != "http://media-broker:8091" ||
		cfg.Sticker.Worker.URL != "http://sticker-broker:8098" ||
		(cfg.Script.Enabled && cfg.Script.Socket != "/run/script-ipc/evaluate.sock") {
		return nil, errors.New("runtime topology is outside the managed deployment group")
	}
	name, err := instance.ApplicationName("app")
	if err != nil {
		return nil, err
	}
	return store.OpenNamed(ctx, cfg.Database.URL.Value(), name, runtime.PGXTracer())
}

func admissionConfig(db *pgxpool.Pool) (*pgx.ConnConfig, error) {
	config := db.Config().ConnConfig.Copy()
	instance, err := runtimeapp.EnvironmentInstance(false)
	if err != nil {
		return nil, err
	}
	if instance != (runtimeapp.Instance{}) {
		name, nameErr := instance.ApplicationName("admit")
		if nameErr != nil {
			return nil, nameErr
		}
		config.RuntimeParams["application_name"] = name
	}
	return config, nil
}
