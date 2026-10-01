package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/webappurl"
)

// runApp hosts bot, authenticated API, model and Mini App in one process.
// Local order, registration, history and knowledge calls share the HTTP authorizer and services without a
// loopback request. Other adapters retain their current transport boundaries.
func runApp(ctx context.Context, db *pgxpool.Pool, signer identity.Signer,
	logger *slog.Logger, cfg config.Config, runtime *observability.Runtime) (runErr error) {
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
	listener, err := appListener(ctx, cfg)
	if err != nil {
		return err
	}
	defer listener.Close()
	base, err := localOrigin(listener.Addr())
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
			db, deliverySettings.BotID, verify, identityAdapter, identityLinks, signer,
		),
		LegacyOrderBotID:     deliverySettings.BotID,
		InformalName:         broadcastOptions(model).InformalName,
		Delivery:             deliverySettings,
		AnnouncementBindings: &destination.Bindings{},
		DestinationResolver:  tg,
	})
	authorizer := applicationauth.Authorizer{DB: db, Verify: applicationauth.VerifyOwner(verify)}
	b := &bot.Bot{
		Delivery:            deliverySettings,
		OrderEventID:        cfg.Orders.ActiveEvent,
		DB:                  db,
		Logger:              logger,
		Observer:            runtime,
		Model:               model,
		AssistantDailyLimit: cfg.Model.AssistantDailyLimit,
		CreditsEnforce:      cfg.Credits.Enforce,
		HistoryLimit:        cfg.History.Recent,
		WebAppURL:           cfg.Telegram.WebAppURL,
		API: combinedClient(
			base,
			telemetryClient(runtime, "api", apiClientTimeout),
			services,
			authorizer,
		),
		TG: tg,
	}
	configureBotLineup(b, cfg)
	if err = configureBotAuth(ctx, b, cfg, signer); err != nil {
		return err
	}
	configureLocalHost(&b.Host, services, authorizer)
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
	fatal := newFatalLatch(cancel)
	stopMaintenance, err := startProductMaintenance(ctx, db, logger, cfg, services, runtime, fatal.report)
	if err != nil {
		return fatal.result(err)
	}
	// Maintenance workers join before the retained failure is read. A fatal
	// cancels ctx, which runAppServers already propagates to bot and HTTP.
	defer func() { stopMaintenance(); runErr = fatal.result(runErr) }()
	if err = configureBrowserAuth(b, cfg, base); err != nil {
		return err
	}
	mux := appMux(b, cfg)
	coreHandler, closeProvisioning, err := configureCoreProvisioning(ctx,
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
	mux.Handle("/", coreHandler)
	return runAppServers(ctx, cancel, b, func(live context.Context) error {
		return serveListener(live, listener, telemetryHandler(runtime, publicHandler(mux, cfg)), logger, cfg)
	})
}

// runAppServers joins the bot worker before caller-owned resources are closed.
func runAppServers(
	ctx context.Context,
	cancel context.CancelFunc,
	b *bot.Bot,
	serve func(context.Context) error,
) error {
	finished := make(chan error, 1)
	go func() { finished <- b.Run(ctx); cancel() }()
	err := serve(ctx)
	cancel()
	return errors.Join(err, <-finished)
}
func combinedClient(
	base string, transport *http.Client, services appservices.Services, authorizer applicationauth.Authorizer,
) appclient.Client {
	return appclient.Client{
		Base: base, HTTP: transport,
		LocalHistory:   &appclient.LocalHistory{Service: services.Conversation, Authorizer: authorizer},
		LocalKnowledge: &appclient.LocalKnowledge{Service: services.Knowledge, Authorizer: authorizer},
		LocalOrders:    &appclient.LocalOrders{Service: services.Orders, Authorizer: authorizer},
		LocalRegistration: &appclient.LocalRegistration{
			Service: services.Registration, Profile: services.PassProfiles,
			Files: services.Orders, Batches: services.DerivedMutations, Authorizer: authorizer,
		},
	}
}

func appListener(ctx context.Context, cfg config.Config) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)))
}

func appMux(b *bot.Bot, cfg config.Config) *http.ServeMux {
	gateway := appGateway(b.API, b.Onboarding, b.BrowserAuth, b.TG.Token, cfg).Handler()
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

func configureLocalHost(host *appclient.Host, services appservices.Services, authorizer applicationauth.Authorizer) {
	host.LocalMemoryReadState = &appclient.LocalMemoryReadState{
		Service:    services.MemoryReadState,
		Authorizer: authorizer,
	}
	host.LocalBotDelivery = &appclient.LocalBotDelivery{Service: services.BotDelivery, Authorizer: authorizer}
	host.LocalHistory = &appclient.LocalHistory{Service: services.Conversation, Authorizer: authorizer}
	host.LocalKnowledge = &appclient.LocalKnowledge{Service: services.Knowledge, Authorizer: authorizer}
	host.LocalDerived = &appclient.LocalDerived{Service: services.DerivedMutations, Authorizer: authorizer}
}
