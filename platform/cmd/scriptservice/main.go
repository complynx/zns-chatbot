// scriptservice exposes the isolated script supervisor only on a Unix socket.
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
)

const socket = "/run/script-ipc/evaluate.sock"

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	os.Clearenv()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "health" {
			return errors.New("unknown scriptservice command")
		}
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket)
		if err != nil {
			return err
		}
		return connection.Close()
	}
	service, err := scriptservice.New("/usr/local/bin/scriptworker")
	if err != nil {
		return err
	}
	// This fixed path is inside the dedicated, bounded IPC volume.
	if err = os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	//nolint:gosec // Dedicated IPC group connects app UID 10001 to evaluator UID 10002; volume is 0770 and unpublished.
	if err = os.Chmod(socket, 0660); err != nil {
		return err
	}
	const timeout = 5 * time.Second
	server := &http.Server{Handler: service, ReadHeaderTimeout: time.Second,
		ReadTimeout: timeout, WriteTimeout: timeout, IdleTimeout: timeout,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		service.Stop()
		// Close ordinary HTTP connections, including incomplete request bodies.
		// Hijacked sessions and their children are joined by the service below.
		closeErr := server.Close()
		shutdown, stop := context.WithTimeout(context.Background(), timeout)
		defer stop()
		stopped <- errors.Join(closeErr, service.Shutdown(shutdown))
	}()
	err = server.Serve(listener)
	close(done)
	shutdownErr := <-stopped
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
}
