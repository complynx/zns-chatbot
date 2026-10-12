// Shuttle serves durable Telegram ingress and the private agent API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/propagation"

	"github.com/complynx/shuttle/internal/api"
	"github.com/complynx/shuttle/internal/config"
	"github.com/complynx/shuttle/internal/db"
	"github.com/complynx/shuttle/internal/identity"
	"github.com/complynx/shuttle/internal/loom"
	"github.com/complynx/shuttle/internal/observability"
	"github.com/complynx/shuttle/internal/task"
	"github.com/complynx/shuttle/internal/telegram"
)

const (
	hardShutdownTimeout  = 5 * time.Second
	headerTimeout        = 5 * time.Second
	serverCount          = 2
	shutdownParticipants = 3
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err.Error())
		os.Exit(1)
	}
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
	h := &http.Client{Timeout: c.HTTPTimeout}
	iam, err := identity.New(identityOptions(c, settings), h)
	if err != nil {
		return nil, nil, nil, err
	}
	tg, err := telegram.New(c.Telegram.BaseURL, c.Telegram.Token, h)
	if err != nil {
		return nil, nil, nil, err
	}
	obs, err := observability.New(
		ctx,
		observability.Options{
			OTLPEndpoint: c.OTel.Endpoint,
			ServiceName:  c.OTel.ServiceName,
			SampleRatio:  c.OTel.SampleRatio,
			MaxQueueSize: c.OTel.MaxQueueSize,
			Timeout:      c.OTel.Timeout,
		},
	)
	if err != nil {
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
	engine, err := task.New(
		store,
		iam,
		lc,
		tg,
		obs,
		logger,
		task.Options{
			BotID:          c.Telegram.BotID,
			MaxWorkers:     settings.Document.MaxParallelTasks,
			PollInterval:   time.Second,
			AttemptTimeout: c.HTTPTimeout,
			LeaseDuration:  c.HTTPTimeout + hardShutdownTimeout,
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
			BotID:          c.Telegram.BotID,
			WebhookSecret:  c.Telegram.WebhookSecret,
			OrganizationID: c.Identity.OrganizationID,
			ActorID:        c.Identity.ActorID,
			Issuer:         c.Identity.Issuer,
			LoomAudience:   c.Identity.Audience,
		},
	)
	if err != nil {
		_ = obs.Shutdown(ctx)
		return nil, nil, nil, err
	}
	return engine, handlers, obs, nil
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
	public := server(c, obs.Middleware(handlers.PublicHandler()))
	private := server(c, obs.Middleware(privateMux))
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
		cancel()
	case err = <-failures:
		cancel()
	}
	handlers.SetAccepting(false)
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), c.ShutdownTimeout)
	defer stop()
	drain(shutdown, engine, public, private)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("HTTP server stopped unexpectedly")
	}
	return nil
}

func server(c config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
		ReadTimeout:       c.HTTPTimeout,
		WriteTimeout:      c.HTTPTimeout,
		IdleTimeout:       time.Minute,
	}
}

func drain(ctx context.Context, engine *task.Service, public, private *http.Server) {
	done := make(chan struct{}, shutdownParticipants)
	go func() { _ = public.Shutdown(ctx); _ = public.Close(); done <- struct{}{} }()
	go func() { _ = private.Shutdown(ctx); _ = private.Close(); done <- struct{}{} }()
	go func() { _ = engine.Stop(ctx); done <- struct{}{} }()
	for range shutdownParticipants {
		select {
		case <-done:
		case <-ctx.Done():
			_ = public.Close()
			_ = private.Close()
			return
		}
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
