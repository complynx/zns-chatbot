package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func TestHTTPDrainCannotRenewTelemetryShutdown(t *testing.T) {
	t.Parallel()
	exportStarted, releaseExport := make(chan struct{}), make(chan struct{})
	var exportOnce sync.Once
	exporter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exportOnce.Do(func() { close(exportStarted) })
		select {
		case <-releaseExport:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer exporter.Close()
	defer close(releaseExport)
	runtime, err := observability.New(t.Context(), observability.Config{
		Enabled: true, Endpoint: exporter.URL, SampleRatio: 1,
	})
	require.NoError(t, err)
	_, finishSpan := runtime.Start(t.Context(), "server")
	finishSpan(nil)
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, finish := runtimeapp.WithShutdownBudget(parent, 250*time.Millisecond)
	defer finish()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	started, release := make(chan struct{}), make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveListener(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = io.WriteString(w, "complete")
		}), slog.New(slog.DiscardHandler), config.Config{Shutdown: config.Shutdown{Drain: time.Second}})
	}()
	response := make(chan string, 1)
	go func() {
		client := http.Client{Timeout: time.Second}
		result, getErr := client.Get("http://" + listener.Addr().String())
		if getErr != nil {
			response <- getErr.Error()
			return
		}
		defer result.Body.Close()
		body, _ := io.ReadAll(result.Body)
		response <- string(body)
	}()
	awaitRuntimeServer(t, started)
	cancel()
	close(release)
	require.Equal(t, "complete", <-response)
	require.NoError(t, <-serverDone, "accepted request must finish before resources close")
	cutoff, stop := runtimeapp.CompletionContext(ctx, time.Second)
	defer stop()
	flushed := make(chan error, 1)
	go func() { flushed <- flushTelemetry(ctx, runtime, time.Second) }()
	awaitRuntimeServer(t, exportStarted)
	<-cutoff.Done()
	select {
	case err = <-flushed:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("telemetry renewed its window after the shared shutdown deadline")
	}
}
