// telemetrylab is a loopback-only collector for synthetic local acceptance.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/telemetrylab"
)

func main() {
	if run() != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	lab := &telemetrylab.Lab{}
	const timeout = 5 * time.Second
	server := &http.Server{Addr: "127.0.0.1:8116", Handler: lab.Handler(), ReadHeaderTimeout: time.Second,
		ReadTimeout: timeout, WriteTimeout: timeout, IdleTimeout: timeout}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		shutdown, stop := context.WithTimeout(context.Background(), timeout)
		defer stop()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	err := server.ListenAndServe()
	close(done)
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
