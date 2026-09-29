package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func serve(ctx context.Context, handler http.Handler, logger *slog.Logger, cfg config.Config) error {
	listener, err := (&net.ListenConfig{}).Listen(
		ctx,
		"tcp",
		net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
	)
	if err != nil {
		return err
	}
	defer listener.Close()
	return serveListener(ctx, listener, handler, logger, cfg)
}

func serveListener(ctx context.Context, listener net.Listener, handler http.Handler,
	logger *slog.Logger, cfg config.Config) error {
	requests, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	stopLossWatcher := cancelRequestsOnLoss(ctx, cancelRequests)
	defer stopLossWatcher()
	active := &activeRequests{next: handler}
	server := &http.Server{
		Handler: active, ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout: cfg.Server.ReadTimeout, WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout: cfg.Server.IdleTimeout,
		BaseContext: func(net.Listener) context.Context { return requests },
	}
	ended, drained := make(chan struct{}), make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			active.stop()
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Shutdown.Drain)
			defer cancel()
			err := server.Shutdown(shutdown)
			if err != nil {
				cancelRequests()
				_ = server.Close()
			}
			drained <- err
		case <-ended:
			drained <- nil
		}
	}()
	logger.InfoContext(ctx, "listening", "address", listener.Addr().String())
	err := server.Serve(listener)
	close(ended)
	drainErr := <-drained
	active.stop()
	cancelRequests()
	active.wait.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, drainErr)
}

// Handlers must honor request cancellation. Stop admission before joining so
// an accepted connection cannot add a handler while shutdown waits for zero.
type activeRequests struct {
	next    http.Handler
	mu      sync.Mutex
	wait    sync.WaitGroup
	stopped bool
}

func (a *activeRequests) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	a.wait.Add(1)
	a.mu.Unlock()
	defer a.wait.Done()
	a.next.ServeHTTP(w, r)
}

func (a *activeRequests) stop() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
}

// Keep watching through a normal drain: admission can be lost after SIGTERM.
func cancelRequestsOnLoss(ctx context.Context, cancel context.CancelFunc) func() {
	if runtimeapp.Lost(ctx) == nil {
		return func() {}
	}
	stop, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-runtimeapp.Lost(ctx):
			cancel()
		case <-stop:
		}
	}()
	return func() { close(stop); <-joined }
}
