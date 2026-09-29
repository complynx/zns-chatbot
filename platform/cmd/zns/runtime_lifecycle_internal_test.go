package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func TestRuntimeServerLossCancelsAcceptedRequest(t *testing.T) {
	t.Parallel()
	for _, gracefulFirst := range []bool{false, true} {
		name := "running"
		if gracefulFirst {
			name = "draining"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRuntimeServerLoss(t, gracefulFirst)
		})
	}
}

func testRuntimeServerLoss(t *testing.T, gracefulFirst bool) {
	t.Helper()
	db := runtimeServerDatabase(t)
	admissionConfig := db.Config().ConnConfig.Copy()
	admissionConfig.RuntimeParams["application_name"] = "zns_synthetic_server_admission"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finishRequest := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(finishRequest)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-release
		_, _ = io.WriteString(w, "stopped")
	})
	finished := make(chan error, 1)
	go func() {
		finished <- runtimeapp.Run(ctx, admissionConfig, runtimeapp.App, func(work context.Context) error {
			return serveListener(work, listener, handler, slog.New(slog.DiscardHandler),
				config.Config{Shutdown: config.Shutdown{Drain: 20 * time.Second}})
		})
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := http.Client{Timeout: 10 * time.Second}
		response, requestErr := client.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	awaitRuntimeServer(t, started)
	if gracefulFirst {
		cancel()
	}
	var terminated bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT pg_terminate_backend(pid)
 FROM pg_stat_activity WHERE datname=current_database()
 AND application_name='zns_synthetic_server_admission'`).Scan(&terminated))
	require.True(t, terminated)
	awaitRuntimeServer(t, cancelled)
	select {
	case err = <-finished:
		t.Fatalf("runtime returned before request cleanup: %v", err)
	default:
	}
	finishRequest()
	require.ErrorIs(t, <-finished, runtimeapp.ErrLost)
	awaitRuntimeServer(t, clientDone)
}

func runtimeServerDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_runtime_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	options, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	options.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), options)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	return db
}

func awaitRuntimeServer(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime request did not reach the expected boundary")
	}
}
