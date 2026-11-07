package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestDrainAllowsAnActiveRequestAndWorkersToFinish(t *testing.T) {
	t.Parallel()
	lifecycle, stopLifecycle := context.WithCancel(context.Background())
	defer stopLifecycle()
	entered, release := make(chan struct{}), make(chan struct{})
	public := httptest.NewUnstartedServer(nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
			http.Error(w, "cancelled before drain", http.StatusServiceUnavailable)
			return
		case <-release:
		}
		if r.Context().Err() != nil {
			http.Error(w, "cancelled before drain", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("finished"))
	})
	public.Config = server(lifecycle, config.Config{HTTPTimeout: 5 * time.Second}, handler)
	public.Start()
	t.Cleanup(public.Close)
	releaseRequest := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseRequest)
	private := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(private.Close)
	type response struct {
		body string
		err  error
	}
	responseDone := make(chan response, 1)
	go func() {
		result, err := public.Client().Get(public.URL)
		if err != nil {
			responseDone <- response{err: err}
			return
		}
		defer result.Body.Close()
		body, err := io.ReadAll(result.Body)
		responseDone <- response{string(body), err}
	}()
	<-entered
	stopLifecycle()
	workersEntered, workersRelease := make(chan struct{}), make(chan struct{})
	releaseWorkers := sync.OnceFunc(func() { close(workersRelease) })
	t.Cleanup(releaseWorkers)
	done := make(chan drainSummary, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		done <- drain(ctx, func(context.Context) error {
			close(workersEntered)
			<-workersRelease
			return nil
		}, public.Config, private.Config)
	}()
	<-workersEntered
	select {
	case <-done:
		t.Fatal("drain returned while a request and workers were active")
	default:
	}
	releaseRequest()
	got := <-responseDone
	require.NoError(t, got.err)
	require.Equal(t, "finished", got.body)
	releaseWorkers()
	report := <-done
	require.Equal(t, drainCompleted, report.Outcome)
	require.Equal(t, drainCompleted, report.PublicHTTP)
	require.Equal(t, drainCompleted, report.PrivateHTTP)
	require.Equal(t, drainCompleted, report.Workers)
	require.GreaterOrEqual(t, report.Duration, time.Duration(0))
	_, err := public.Client().Get(public.URL)
	require.Error(t, err)
	_, err = private.Client().Get(private.URL)
	require.Error(t, err)
}

func TestExpiredDrainClosesHTTPAndDoesNotWaitForLateWorkers(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	public := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	t.Cleanup(public.Close)
	t.Cleanup(func() { close(release) })
	private := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(private.Close)
	requestDone := make(chan error, 1)
	go func() {
		result, err := public.Client().Get(public.URL)
		if result != nil {
			_ = result.Body.Close()
		}
		requestDone <- err
	}()
	<-entered
	workersRelease, workersReturned := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(workersRelease); <-workersReturned })
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	report := drain(ctx, func(context.Context) error {
		<-workersRelease
		close(workersReturned)
		return nil
	}, public.Config, private.Config)
	require.Equal(t, "deadline", report.Outcome)
	require.Equal(t, drainUnfinished, report.Workers)
	require.Error(t, <-requestDone)
}

func TestDrainFailureIsClassifiedWithoutLoggingPrivateErrors(t *testing.T) {
	t.Parallel()
	public, private := &http.Server{}, &http.Server{}
	report := drain(context.Background(), func(context.Context) error {
		return errors.New("private-token private-payload")
	}, public, private)
	require.Equal(t, "error", report.Outcome)
	require.Equal(t, "error", report.Workers)
	var output bytes.Buffer
	logDrain(context.Background(), newLogger(&output), report, time.Second)
	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.Equal(t, "application drain finished", record["msg"])
	require.Equal(t, "WARN", record["level"])
	require.Equal(t, "error", record["outcome"])
	require.Equal(t, "error", record["workers"])
	require.InDelta(t, 1000, record["budget_ms"], 0.001)
	require.NotContains(t, output.String(), "private-token")
	require.NotContains(t, output.String(), "private-payload")
}

func TestExpiredBudgetRemainsVisibleWithSuccessfulCallbacks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	report := drain(ctx, func(context.Context) error { return nil }, &http.Server{}, &http.Server{})
	require.Equal(t, "deadline", report.Outcome)
}
