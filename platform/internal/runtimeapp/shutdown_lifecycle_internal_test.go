package runtimeapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type delayedShutdownAdmission struct {
	testAdmission
}

func (a *delayedShutdownAdmission) Close(ctx context.Context) error {
	<-ctx.Done()
	close(a.closed)
	return nil
}

func TestAdmissionCleanupCannotRenewNormalShutdown(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner, finish := WithShutdownBudget(parent, 40*time.Millisecond)
	defer finish()
	admission := &delayedShutdownAdmission{testAdmission{done: make(chan struct{}), closed: make(chan struct{})}}
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- runAdmitted(owner, admission, func(work context.Context) error {
			close(started)
			<-work.Done()
			return nil
		})
	}()
	awaitLifecycle(t, started)
	cancel()
	require.ErrorIs(t, <-finished, context.DeadlineExceeded, "late nil Close cannot mean complete normal shutdown")
	awaitLifecycle(t, admission.closed)
}

func TestShutdownRetainsAdmissionUntilLateWorkerJoin(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner, finish := WithShutdownBudget(parent, 30*time.Millisecond)
	defer finish()
	admission := &testAdmission{done: make(chan struct{}), closed: make(chan struct{})}
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- runAdmitted(owner, admission, func(work context.Context) error {
			close(started)
			<-work.Done()
			<-release
			return nil
		})
	}()
	awaitLifecycle(t, started)
	cancel()
	BeginShutdown(owner)
	cutoff, stop := CompletionContext(owner, time.Second)
	defer stop()
	<-cutoff.Done()
	select {
	case <-admission.closed:
		t.Error("expired shutdown released admission before worker join")
	default:
	}
	close(release)
	require.ErrorIs(t, <-finished, context.DeadlineExceeded)
	awaitLifecycle(t, admission.closed)
}
