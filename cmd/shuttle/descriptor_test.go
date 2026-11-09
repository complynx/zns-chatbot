package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestDisabledDescriptorAllocatesNoDependencies(t *testing.T) {
	t.Parallel()
	worker, err := embeddedDescriptor(t.Context(), config.Config{}, nil, nil)
	require.NoError(t, err)
	require.Nil(t, worker)
}

func TestDescriptorFailureDoesNotStopApplication(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine, background, servers := make(chan error), make(chan error), make(chan error)
	done := make(chan error, 1)
	logger := slog.New(slog.DiscardHandler)
	go func() { done <- waitApplication(ctx, cancel, engine, background, servers, logger) }()
	select {
	case background <- errors.New("descriptor journal unavailable"):
	case <-time.After(2 * time.Second):
		t.Fatal("application did not observe descriptor failure")
	}
	select {
	case engine <- errors.New("core worker unavailable"):
	case <-done:
		t.Fatal("optional descriptor stopped the application")
	case <-time.After(2 * time.Second):
		t.Fatal("application stopped observing core workers")
	}
	select {
	case err := <-done:
		require.ErrorContains(t, err, "core worker unavailable")
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("core failure did not stop the application")
	}
}
