package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func TestRuntimeLifecycleOwnsStartupAndDrain(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, draining, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- runtimeapp.Run(ctx, runtimeAdmissionConfig(db, "lifecycle"), runtimeapp.App,
			func(work context.Context) error {
				close(started)
				<-work.Done()
				close(draining)
				<-release
				return nil
			})
	}()
	requireAdmissionSignal(t, started)
	called := false
	err := runtimeapp.Run(t.Context(), runtimeAdmissionConfig(db, "denied"), runtimeapp.API,
		func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, runtimeapp.ErrBusy)
	require.False(t, called, "conflicting runtime must not construct services or start writers")
	cancel()
	requireAdmissionSignal(t, draining)
	contender, err := runtimeapp.Acquire(t.Context(), runtimeAdmissionConfig(db, "draining"), runtimeapp.Bot)
	require.ErrorIs(t, err, runtimeapp.ErrBusy)
	require.Nil(t, contender)
	close(release)
	require.NoError(t, <-finished)
	requireAdmissionSessions(t, db, "lifecycle", 0)
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "after_drain"), runtimeapp.App)
}

func TestRuntimeLifecycleLossDuringDrainCancelsRemainingWork(t *testing.T) {
	t.Parallel()
	db := database(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, draining := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- runtimeapp.Run(ctx, runtimeAdmissionConfig(db, "drain_loss"), runtimeapp.API,
			func(work context.Context) error {
				close(started)
				<-work.Done()
				close(draining)
				<-runtimeapp.Lost(work)
				return nil
			})
	}()
	requireAdmissionSignal(t, started)
	cancel()
	requireAdmissionSignal(t, draining)
	terminateRuntimeAdmission(t, db, "drain_loss")
	require.ErrorIs(t, <-finished, runtimeapp.ErrLost)
	requireAdmissionSessions(t, db, "drain_loss", 0)
}
