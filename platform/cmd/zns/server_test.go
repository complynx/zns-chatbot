package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestServerShutdownDrainsAcceptedRequest(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	finished := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
			_, _ = io.WriteString(w, "complete")
		case <-r.Context().Done():
			http.Error(w, "cancelled", http.StatusServiceUnavailable)
		}
	})
	go func() {
		finished <- serveListener(ctx, listener, handler, slog.New(slog.DiscardHandler),
			config.Config{Shutdown: config.Shutdown{Drain: time.Second}})
	}()
	response := make(chan string, 1)
	go func() {
		client := http.Client{Timeout: 5 * time.Second}
		result, getErr := client.Get("http://" + listener.Addr().String())
		if getErr != nil {
			response <- getErr.Error()
			return
		}
		defer result.Body.Close()
		body, _ := io.ReadAll(result.Body)
		response <- string(body)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err = <-finished:
		t.Fatalf("server returned before request was released: %v", err)
	default:
	}
	close(release)
	require.Equal(t, "complete", <-response)
	require.NoError(t, <-finished)
}

func TestServerShutdownCancelsAfterDrainBudget(t *testing.T) {
	t.Parallel()
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	finished := make(chan error, 1)
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-release
	})
	go func() {
		finished <- serveListener(ctx, listener, handler, slog.New(slog.DiscardHandler),
			config.Config{Shutdown: config.Shutdown{Drain: 50 * time.Millisecond}})
	}()
	clientFinished := make(chan struct{})
	go func() {
		defer close(clientFinished)
		client := http.Client{Timeout: 5 * time.Second}
		response, getErr := client.Get("http://" + listener.Addr().String())
		if getErr == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("active request was not cancelled after drain")
	}
	select {
	case err = <-finished:
		t.Fatalf("server returned before cancelled handler cleanup: %v", err)
	default:
	}
	close(release)
	require.ErrorIs(t, <-finished, context.DeadlineExceeded)
	<-clientFinished
}

func TestLocalOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ ip, want string }{
		{"0.0.0.0", "http://127.0.0.1:8080"}, {"::", "http://[::1]:8080"},
		{"127.0.0.1", "http://127.0.0.1:8080"},
	} {
		origin, err := localOrigin(&net.TCPAddr{IP: net.ParseIP(tc.ip), Port: 8080})
		require.NoError(t, err)
		require.Equal(t, tc.want, origin)
	}
	_, err := localOrigin(badAddress{})
	require.Error(t, err)
}

type badAddress struct{}

func (badAddress) Network() string { return "invalid" }
func (badAddress) String() string  { return "invalid address" }
