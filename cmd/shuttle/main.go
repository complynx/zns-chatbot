// Shuttle serves durable Telegram ingress and the private agent API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/propagation"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/observability"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	hardShutdownTimeout  = 5 * time.Second
	headerTimeout        = 5 * time.Second
	serverCount          = 2
	shutdownParticipants = 3
)

func main() {
	logger := newLogger(os.Stdout)
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err.Error())
		os.Exit(1)
	}
}

func newLogger(output io.Writer) *slog.Logger {
	options := &slog.HandlerOptions{}
	if os.Getenv("SHUTTLE_LOG_LEVEL") == "debug" {
		options.Level = slog.LevelDebug
	}
	return slog.New(observability.WithTraceContext(slog.NewJSONHandler(output, options)))
}

func run(logger *slog.Logger) error {
	path := flag.String("config", "config/shuttle.yaml", "Startup YAML path")
	migrate := flag.Bool("migrate", false, "Apply database migrations and exit")
	flag.Parse()
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	defer watchdog(ctx)()
	startup, stopStartup := context.WithTimeout(ctx, c.HTTPTimeout)
	defer stopStartup()
	store, err := db.Open(startup, c.DatabaseURL)
	if err != nil {
		return errors.New("open application database")
	}
	defer store.Close()
	if *migrate {
		return migrateDatabase(startup, store)
	}
	settings, err := store.Settings(startup)
	if err != nil {
		return errors.New("load runtime settings; run -migrate first")
	}
	engine, handlers, obs, err := compose(startup, c, store, settings, logger)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), c.OTel.Timeout)
		defer stop()
		_ = obs.Shutdown(cleanup)
	}()
	return serve(ctx, cancel, c, engine, handlers, obs, logger)
}

func migrateDatabase(ctx context.Context, store *db.Store) error {
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate application schema: %w", err)
	}
	return task.MigrateQueue(ctx, store.Pool())
}

func compose(
	ctx context.Context,
	c config.Config,
	store *db.Store,
	settings db.VersionedSettings,
	logger *slog.Logger,
) (*task.Service, *api.Server, *observability.Telemetry, error) {
	details, err := registrationDetailsProtector(c)
	if err != nil {
		return nil, nil, nil, err
	}
	h := &http.Client{Timeout: c.HTTPTimeout}
	iam, err := identity.New(identityOptions(c, settings), h)
	if err != nil {
		return nil, nil, nil, err
	}
	tg, err := telegram.New(c.Telegram.BaseURL, c.Telegram.Token, h)
	if err != nil {
		return nil, nil, nil, err
	}
	obs, err := observability.New(ctx, telemetryOptions(c))
	if err != nil {
		return nil, nil, nil, err
	}
	if err = observeDependencies(obs, iam, store); err != nil {
		_ = obs.Shutdown(ctx)
		return nil, nil, nil, err
	}
	lc, err := loom.New(
		c.Loom.BaseURL,
		&http.Client{
			Timeout:   c.HTTPTimeout,
			Transport: propagationTransport{base: http.DefaultTransport, propagator: obs.Propagator()},
		},
	)
	if err != nil {
		_ = obs.Shutdown(ctx)
		return nil, nil, nil, err
	}
	miniAppURL := c.MiniAppURL()
	renderer, err := avatar.NewExecutable(c.AvatarExecutable)
	if err != nil {
		_ = obs.Shutdown(ctx)
		return nil, nil, nil, err
	}
	engine, err := task.New(
		store,
		iam,
		lc,
		tg,
		obs,
		logger,
		task.Options{
			AvatarRenderer:             renderer,
			BotID:                      c.Telegram.BotID,
			MiniAppURL:                 miniAppURL,
			RegistrationDetailsEnabled: details != nil,
			MaxWorkers:                 settings.Document.MaxParallelTasks,
			PollInterval:               time.Second,
			AttemptTimeout:             c.HTTPTimeout,
			LeaseDuration:              c.HTTPTimeout + hardShutdownTimeout,
		},
	)
	if err != nil {
		_ = obs.Shutdown(ctx)
		return nil, nil, nil, err
	}
	handlers, err := api.New(
		store,
		iam,
		engine,
		api.Options{
			RegistrationDetails: details,
			TelegramPrelink: api.TelegramPrelinkOptions{
				Issuer:        c.TelegramPrelink.Issuer,
				JWKSURL:       c.TelegramPrelink.JWKSURL,
				Audience:      c.TelegramPrelink.Audience,
				ProviderID:    c.TelegramPrelink.ProviderID,
				ServiceBearer: c.TelegramPrelink.ServiceBearer,
				HTTP:          h,
			},
			APIAudience:           c.Identity.APIAudience,
			MiniAppOrigin:         c.MiniAppOrigin,
			MiniAppAllowLocalHTTP: c.MiniAppAllowLocalHTTP,
			MiniAppBotToken:       c.Telegram.Token,
			AvatarRenderer:        renderer,
			Telegram:              tg,
			BotID:                 c.Telegram.BotID,
			WebhookSecret:         c.Telegram.WebhookSecret,
			OrganizationID:        c.Identity.OrganizationID,
			ActorID:               c.Identity.ActorID,
			Issuer:                c.Identity.Issuer,
			LoomAudience:          c.Identity.Audience,
		},
	)
	if err != nil {
		_ = obs.Shutdown(ctx)
		return nil, nil, nil, err
	}
	return engine, handlers, obs, nil
}

func telemetryOptions(c config.Config) observability.Options {
	return observability.Options{
		OTLPEndpoint: c.OTel.Endpoint, ServiceName: c.OTel.ServiceName, SampleRatio: c.OTel.SampleRatio,
		MaxQueueSize: c.OTel.MaxQueueSize, Timeout: c.OTel.Timeout,
	}
}

func registrationDetailsProtector(c config.Config) (*registrationdetails.Protector, error) {
	key, err := c.RegistrationDetailsSecret()
	if err != nil || len(key) == 0 {
		return nil, err
	}
	return registrationdetails.NewProtector(key)
}

func identityOptions(c config.Config, settings db.VersionedSettings) identity.Options {
	i := c.Identity
	return identity.Options{
		Issuer:                    i.Issuer,
		OrganizationID:            i.OrganizationID,
		BotID:                     c.Telegram.BotID,
		LoomAudience:              i.Audience,
		APIAudience:               i.APIAudience,
		ActorID:                   i.ActorID,
		ActorClientCredentials:    identity.ClientCredentials{ID: i.ActorClientID, Secret: i.ActorClientSecret},
		ExchangeClientCredentials: identity.ClientCredentials{ID: i.ClientID, Secret: i.ClientSecret},
		IntrospectionClientCredentials: identity.ClientCredentials{
			ID:     i.IntrospectionClientID,
			Secret: i.IntrospectionClientSecret,
		},
		PositiveCacheTTL: time.Duration(settings.Document.AuthCacheTTLSeconds) * time.Second,
	}
}

func serve(
	ctx context.Context,
	cancel context.CancelFunc,
	c config.Config,
	engine *task.Service,
	handlers *api.Server,
	obs *observability.Telemetry,
	logger *slog.Logger,
) error {
	listenConfig := net.ListenConfig{}
	publicListener, err := listenConfig.Listen(ctx, "tcp", c.HTTPAddress)
	if err != nil {
		return errors.New("listen on public address")
	}
	defer publicListener.Close()
	privateListener, err := listenConfig.Listen(ctx, "tcp", c.InternalAddress)
	if err != nil {
		return errors.New("listen on internal address")
	}
	defer privateListener.Close()
	privateMux := http.NewServeMux()
	privateMux.Handle("/metrics", obs.MetricsHandler())
	privateMux.Handle("/", handlers.InternalHandler())
	public := server(ctx, c, obs.Middleware(handlers.PublicHandler()))
	private := server(ctx, c, obs.Middleware(privateMux))
	if err = engine.Start(ctx); err != nil {
		return err
	}
	failures := make(chan error, serverCount)
	go func() { failures <- public.Serve(publicListener) }()
	go func() { failures <- private.Serve(privateListener) }()
	logger.InfoContext(ctx, "application ready")
	select {
	case <-ctx.Done():
	case err = <-engine.Failures():
		err = fmt.Errorf("task worker stopped: %w", err)
		cancel()
	case err = <-failures:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			err = errors.New("HTTP server stopped unexpectedly")
		}
		cancel()
	}
	handlers.SetAccepting(false)
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), c.ShutdownTimeout)
	defer stop()
	summary := drain(shutdown, engine.Stop, public, private)
	logDrain(shutdown, logger, summary, c.ShutdownTimeout)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func server(ctx context.Context, c config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		// SIGTERM starts drain; closing connections cancels unfinished requests.
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
		ReadTimeout:       c.HTTPTimeout,
		WriteTimeout:      c.HTTPTimeout,
		IdleTimeout:       time.Minute,
	}
}

// watchdog remains active through deferred cleanup and shares no mutable state.
func watchdog(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		timer := time.NewTimer(hardShutdownTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			os.Exit(1)
		case <-done:
		}
	}()
	return func() { close(done) }
}

// Only Loom gets propagated headers; Telegram token-bearing URLs are never traced.
type propagationTransport struct {
	base       http.RoundTripper
	propagator propagation.TextMapPropagator
}

func (t propagationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	t.propagator.Inject(request.Context(), propagation.HeaderCarrier(cloned.Header))
	return t.base.RoundTrip(cloned)
}

func observeDependencies(obs *observability.Telemetry, iam *identity.Client, store *db.Store) error {
	if err := obs.ObserveQueue(func(ctx context.Context) ([]observability.QueueMetric, error) {
		return task.QueueMetrics(ctx, store.Pool())
	}); err != nil {
		return errors.New("initialize queue metrics")
	}
	if err := obs.ObserveIdentity(iam); err != nil {
		return errors.New("initialize identity cache metrics")
	}
	if err := obs.ObserveDatabasePool(store.Pool()); err != nil {
		return errors.New("initialize database pool metrics")
	}
	if err := obs.ObserveTasks(store); err != nil {
		return errors.New("initialize task metrics")
	}
	if err := obs.ObserveOutbox(store); err != nil {
		return errors.New("initialize outbox metrics")
	}
	return nil
}
