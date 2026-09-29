package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

// runApp hosts bot, authenticated API, model and Mini App in one process.
// Local API calls keep the same authenticated HTTP boundary as remote callers.
func runApp(ctx context.Context, db *pgxpool.Pool, signer identity.Signer,
	logger *slog.Logger, cfg config.Config, runtime *observability.Runtime) error {
	verify, _, _, err := runtimeAuth(db, cfg, signer)
	if err != nil {
		return err
	}
	legacyBotID, err := cfg.LegacyOrderBotID()
	if err != nil {
		return err
	}
	model, err := botModel(cfg, runtime, credits.Service{DB: db, Enforce: cfg.Credits.Enforce})
	if err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(
		ctx,
		"tcp",
		net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
	)
	if err != nil {
		return err
	}
	defer listener.Close()
	base, err := localOrigin(listener.Addr())
	if err != nil {
		return err
	}
	b := &bot.Bot{OrderEventID: cfg.Orders.ActiveEvent,
		DB:                  db,
		Logger:              logger,
		Observer:            runtime,
		Model:               model,
		AssistantDailyLimit: cfg.Model.AssistantDailyLimit,
		CreditsEnforce:      cfg.Credits.Enforce,
		HistoryLimit:        cfg.History.Recent,
		WebAppURL:           cfg.Telegram.WebAppURL,
		API: bot.APIClient{
			Base:   base,
			Signer: signer,
			HTTP:   telemetryClient(runtime, "api", apiClientTimeout),
		},
		TG: telegram.Client{
			Base:  cfg.Telegram.BaseURL,
			Token: cfg.Telegram.Token.Value(),
			HTTP:  telemetryClient(runtime, "api", telegramClientTimeout),
		},
	}
	configureBotLineup(b, cfg)
	if err = configureBotAuth(ctx, b, cfg, signer); err != nil {
		return err
	}
	if err = configureTrustedOnboarding(b, cfg); err != nil {
		return err
	}
	closeScripts, err := configureBotHelpers(b, cfg, runtime)
	if err != nil {
		return err
	}
	defer closeScripts()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopMaintenance, err := startProductMaintenance(ctx, db, logger, cfg, legacyBotID)
	if err != nil {
		return err
	}
	defer stopMaintenance()
	if err = configureBrowserAuth(b, cfg, base); err != nil {
		return err
	}
	mux := appMux(b, cfg)
	coreHandler, closeProvisioning, err := configureCoreProvisioning(ctx,
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
	mux.Handle("/", coreHandler)
	finished := make(chan error, 1)
	go func() { finished <- b.Run(ctx); cancel() }()
	err = serveListener(ctx, listener, telemetryHandler(runtime, publicHandler(mux, cfg)), logger, cfg)
	cancel()
	return errors.Join(err, <-finished)
}

func appMux(b *bot.Bot, cfg config.Config) *http.ServeMux {
	gateway := botGateway(b, cfg).Handler()
	mux := http.NewServeMux()
	if b.BrowserAuth != nil {
		mux.Handle("/auth", b.BrowserAuth.LegacyHandler())
	}
	mux.Handle("/miniapp", gateway)
	mux.Handle("/miniapp/", gateway)
	registerMiniAppAliases(mux, gateway)
	return mux
}

func localOrigin(address net.Addr) (string, error) {
	host, port, err := net.SplitHostPort(address.String())
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
		if ip.To4() == nil {
			host = "::1"
		}
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func publicHandler(mux *http.ServeMux, cfg config.Config) http.Handler {
	return browserauth.GuardRouting(webappurl.SubtreeRedirects(cfg.Telegram.WebAppURL, mux))
}
