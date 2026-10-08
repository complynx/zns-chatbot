package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const commandArgCount = 2

const fixtureMode = "fixture"

func main() {
	logger := observability.NewLogger(os.Stderr, observability.LogConfig{})
	if e := run(); e != nil {
		logger.ErrorContext(context.Background(), "stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != commandArgCount {
		return fmt.Errorf("usage: zns app|api|bot|fake|model|migrate|fixture|product-fixture|export-fixture|health")
	}
	if err := rejectRegistrationClockMode(os.Args[1]); err != nil {
		return err
	}
	cfg, err := loadConfig(os.Args[1])
	if err != nil {
		return err
	}
	logger := configuredLogger(os.Stderr, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	ctx, finishShutdown := runtimeapp.WithShutdownBudget(ctx, cfg.Shutdown.Drain)
	defer finishShutdown()
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			runtimeapp.BeginShutdown(ctx)
			cancel()
		case <-ctx.Done():
		}
	}()
	if cfg.ParentStdin {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			runtimeapp.BeginShutdown(ctx)
			cancel()
		}()
	}
	if os.Args[1] == "health" {
		healthContext, c := context.WithTimeout(ctx, cfg.Server.HealthTimeout)
		defer c()
		return sandbox.Ready(healthContext, "http://127.0.0.1:"+strconv.Itoa(cfg.Server.Port))
	}
	if e := cfg.Validate(os.Args[1]); e != nil {
		return e
	}
	runtime, err := observability.New(ctx, observability.Config{Enabled: cfg.OTel.Enabled,
		Endpoint: cfg.OTel.Endpoint, SampleRatio: cfg.OTel.SampleRatio})
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "starting", "command", os.Args[1])
	return runCommand(ctx, logger, cfg, runtime)
}

func runCommand(
	ctx context.Context,
	logger *slog.Logger,
	cfg config.Config,
	runtime *observability.Runtime,
) error {
	return runCommandWithDatabase(ctx, os.Args[1], logger, cfg, runtime, openRuntimeDatabase)
}

func runCommandWithDatabase(
	ctx context.Context,
	command string,
	logger *slog.Logger,
	cfg config.Config,
	runtime *observability.Runtime,
	openDatabase func(context.Context, config.Config, *observability.Runtime) (*pgxpool.Pool, error),
) (result error) {
	ctx, finishShutdown := runtimeapp.WithShutdownBudget(ctx, cfg.Shutdown.Drain)
	defer finishShutdown()
	registrationClock, clockConfigured, clockErr := preflightRegistrationClock(ctx, command, cfg)
	if clockErr != nil {
		return clockErr
	}
	if command == "model" {
		return runModel(ctx, logger, cfg, runtime)
	}
	var db *pgxpool.Pool
	defer func() { result = errors.Join(result, closeRuntime(ctx, cfg, runtime, db)) }()
	var e error
	db, e = openDatabase(ctx, cfg, runtime)
	if e != nil {
		return e
	}
	if e = runtime.RegisterPool(db); e != nil {
		return e
	}
	if clockConfigured {
		if e = registrationClockDatabaseGuard(ctx, db, registrationClock.config); e != nil {
			return e
		}
	}
	work := func(owned context.Context) error {
		if clockConfigured {
			signer := identity.Signer{Key: []byte(cfg.Auth.SigningKey.Value())}
			return runAppWithClock(owned, db, signer, logger, cfg, runtime, registrationClock)
		}
		return runDatabaseCommand(owned, logger, cfg, runtime, db)
	}
	if role, admitted := commandRole(command); admitted {
		admission, configErr := admissionConfig(db)
		if configErr != nil {
			return configErr
		}
		return runtimeapp.Run(ctx, admission, role, work)
	}
	return work(ctx)
}

func runDatabaseCommand(ctx context.Context, logger *slog.Logger, cfg config.Config,
	runtime *observability.Runtime, db *pgxpool.Pool) error {
	signer := identity.Signer{Key: []byte(cfg.Auth.SigningKey.Value())}
	switch os.Args[1] {
	case "migrate":
		if cfg.Env == productionEnvironment {
			return store.Migrate(ctx, db)
		}
		return runFixture(ctx, db, os.Args[1])
	case fixtureMode, "product-fixture", "export-fixture":
		return runFixture(ctx, db, os.Args[1])
	case "api":
		return runAPI(ctx, db, signer, logger, cfg, runtime)
	case "app":
		return runApp(ctx, db, signer, logger, cfg, runtime)
	case "fake":
		fake, fakeError := sandbox.NewWithAttestedOwners(
			ctx,
			db,
			cfg.Telegram.Token.Value(),
			cfg.Auth.SandboxTelegramOwners,
		)
		if fakeError != nil {
			return fakeError
		}
		fake.MiniAppURL = cfg.Sandbox.MiniAppURL
		return serveFake(ctx, fake, telemetryHandler(runtime, fake.Handler()), logger, cfg)
	case "bot":
		deliverySettings, deliveryErr := cfg.DeliverySettings()
		if deliveryErr != nil {
			return deliveryErr
		}
		model, modelErr := botModel(cfg, runtime, credits.Service{DB: db, Enforce: cfg.Credits.Enforce})
		if modelErr != nil {
			return modelErr
		}
		tg, telegramErr := configuredTelegramClient(cfg, runtime, db, deliverySettings)
		if telegramErr != nil {
			return telegramErr
		}
		b := bot.Bot{OrderEventID: cfg.Orders.ActiveEvent,
			Delivery:  deliverySettings,
			Logger:    logger,
			Observer:  runtime,
			WebAppURL: cfg.Telegram.WebAppURL,
			DB:        db,
			API: appclient.Client{
				Base: cfg.Core.URL,

				HTTP: telemetryClient(runtime, "api", apiClientTimeout),
			},
			TG:                  tg,
			Model:               model,
			AssistantDailyLimit: cfg.Model.AssistantDailyLimit,
			CreditsEnforce:      cfg.Credits.Enforce,
			HistoryLimit:        cfg.History.Recent,
		}
		configureBotLineup(&b, cfg)
		if err := configureBotAuth(ctx, &b, cfg, signer); err != nil {
			return err
		}
		if err := configureTrustedOnboarding(&b, cfg); err != nil {
			return err
		}
		closeScripts, scriptErr := configureBotHelpers(&b, cfg, runtime)
		if scriptErr != nil {
			return scriptErr
		}
		defer closeScripts()
		return runBot(ctx, &b, logger, cfg, runtime)
	default:
		return fmt.Errorf("unknown command")
	}
}

func runBot(
	ctx context.Context,
	b *bot.Bot,
	logger *slog.Logger,
	cfg config.Config,
	runtime *observability.Runtime,
) error {
	if err := configureBrowserAuth(b, cfg, ""); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- b.Run(ctx); cancel() }()
	gateway := appGateway(b.API, b.Onboarding, b.BrowserAuth, b.TG.Token, cfg)
	err := serve(ctx, telemetryHandler(runtime, gateway.Handler()), logger, cfg)
	cancel()
	return errors.Join(err, <-finished)
}

func appGateway(client appclient.Client, onboarding func(context.Context, telegram.User) error,
	browser *browserauth.Service, token string, cfg config.Config) miniapp.Gateway {
	return miniapp.Gateway{
		Onboarding: onboarding, WebAppURL: cfg.Telegram.WebAppURL, BrowserAuth: browser,
		API: client, Token: token, EventID: cfg.Orders.ActiveEvent,
		ResolveOrderEvent: func(ctx context.Context, owner, id string) (string, error) {
			order, err := client.OrderByID(ctx, owner, id)
			return order.EventID, err
		},
	}
}

func verifyBotIdentity(ctx context.Context, b *bot.Bot, cfg config.Config) error {
	if cfg.Env != productionEnvironment {
		return nil
	}
	id, err := strconv.ParseInt(cfg.Auth.Zitadel.BotID, 10, 64)
	if err != nil || id <= 0 {
		return errors.New("invalid production Telegram bot identity")
	}
	return telegram.RetryControl(ctx, func(live context.Context) error { return b.TG.VerifyBot(live, id) })
}

func configureBotAV(b *bot.Bot, cfg config.Config, runtime *observability.Runtime) error {
	if cfg.Media.URL == "" && cfg.Media.Secret.Value() == "" {
		return nil
	}
	client, err := mediaclient.New(
		mediaclient.Config{
			CreditsEnforce: cfg.Credits.Enforce,
			URL:            cfg.Media.URL,
			Secret:         cfg.Media.Secret.Value(),
			HTTP:           telemetryClient(runtime, "media.decode", mediaClientTimeout),
		},
	)
	if err != nil {
		return err
	}
	b.AV = client
	return nil
}

func configureBotHelpers(b *bot.Bot, cfg config.Config, runtime *observability.Runtime) (func(), error) {
	if err := configureBotAV(b, cfg, runtime); err != nil {
		return nil, err
	}
	if err := configureBotAssets(b, cfg); err != nil {
		return nil, err
	}
	return configureBotScripts(b, cfg, runtime)
}

// Local Codex runs directly in the sandbox bot; there is no model HTTP endpoint.
func botModel(cfg config.Config, runtime *observability.Runtime, recorders ...credits.Recorder) (agent.Model, error) {
	model, err := unobservedModel(cfg, runtime)
	if err != nil {
		return nil, err
	}
	if len(recorders) > 0 {
		if service, ok := recorders[0].(credits.Service); ok {
			if err = runtime.RegisterModelUsage(service); err != nil {
				return nil, err
			}
		}
		switch typed := model.(type) {
		case agent.Remote:
			typed.Receipts, _ = recorders[0].(credits.RemoteReceiptVerifier)
			model = typed
		case agent.OpenAI:
			typed.Accounting = recorders[0]
			model = typed
		case agent.Codex:
			typed.Accounting = recorders[0]
			model = typed
		}
	}
	return observedModel{next: model, runtime: runtime}, nil
}

func unobservedModel(cfg config.Config, runtime *observability.Runtime) (agent.Model, error) {
	switch cfg.Model.Provider {
	case "scripted":
		return agent.Scripted{}, nil
	case openAIProvider:
		return agent.OpenAI{
			Key:  cfg.Model.OpenAIKey.Value(),
			HTTP: telemetryClient(runtime, "api", openAIClientTimeout),
		}, nil
	case "remote":
		return agent.Remote{
			URL:     cfg.Model.URL,
			HTTP:    telemetryClient(runtime, "api", remoteClientTimeout),
			Secret:  cfg.Model.RemoteSecret.Value(),
			Enforce: cfg.Credits.Enforce,
		}, nil
	case fixtureMode:
		if !cfg.SyntheticOnly || cfg.Model.URL == "" {
			return nil, errors.New("fixture model requires synthetic_only and an explicit URL")
		}
		return sandbox.FixtureRemote{
			URL:            cfg.Model.URL,
			HTTP:           telemetryClient(runtime, "api", remoteClientTimeout),
			AttestedOwners: maps.Clone(cfg.Auth.SandboxTelegramOwners),
		}, nil
	case "codex":
		if !cfg.SyntheticOnly || cfg.Server.Host != "127.0.0.1" {
			return nil, errors.New("codex requires SYNTHETIC_ONLY=true and BIND_HOST=127.0.0.1")
		}
		executable := cfg.Model.CodexExecutable
		if !filepath.IsAbs(executable) {
			return nil, errors.New("CODEX_EXECUTABLE must be absolute")
		}
		if _, err := exec.LookPath(executable); err != nil {
			return nil, errors.New("CODEX_EXECUTABLE is unavailable")
		}
		return agent.Codex{Executable: executable, SyntheticOnly: true}, nil
	default:
		return nil, errors.New("unknown bot MODEL_PROVIDER")
	}
}

func runModel(
	ctx context.Context,
	logger *slog.Logger,
	cfg config.Config,
	runtime *observability.Runtime,
) (result error) {
	var db *pgxpool.Pool
	defer func() { result = errors.Join(result, closeRuntime(ctx, cfg, runtime, db)) }()
	switch cfg.Model.Provider {
	case "scripted":
		return serve(ctx, telemetryHandler(runtime, (&agent.ScriptedServer{}).Handler()), logger, cfg)
	case openAIProvider:
		key := cfg.Model.OpenAIKey.Value()
		if key == "" {
			return fmt.Errorf("OPENAI_API_KEY required for gpt-6-luna")
		}
		var err error
		db, err = store.Open(ctx, cfg.Database.URL.Value(), runtime.PGXTracer())
		if err != nil {
			return err
		}
		model, err := botModel(cfg, runtime, credits.Service{DB: db, Enforce: cfg.Credits.Enforce})
		if err != nil {
			return err
		}
		return serve(
			ctx,
			telemetryHandler(
				runtime,
				agent.AuthenticatedModelHandler(model, cfg.Model.RemoteSecret.Value(), cfg.Credits.Enforce),
			),
			logger,
			cfg,
		)
	default:
		return fmt.Errorf("unknown MODEL_PROVIDER")
	}
}
