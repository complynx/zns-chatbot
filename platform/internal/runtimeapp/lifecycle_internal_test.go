package runtimeapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testAdmission struct {
	done, closed  chan struct{}
	err, closeErr error
}

func (a *testAdmission) Done() <-chan struct{} { return a.done }
func (a *testAdmission) Err() error            { return a.err }
func (a *testAdmission) Close(context.Context) error {
	close(a.closed)
	return errors.Join(a.err, a.closeErr)
}

func awaitLifecycle(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not reach the expected lifecycle boundary")
	}
}

func TestLifecycleRetainsAdmissionUntilWorkersJoin(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	admission := &testAdmission{done: make(chan struct{}), closed: make(chan struct{})}
	started, canceled, joined, done := make(
		chan struct{},
	), make(
		chan struct{},
	), make(
		chan struct{},
	), make(
		chan struct{},
	)
	var result error
	go func() {
		defer close(done)
		result = runAdmitted(ctx, admission, func(work context.Context) error {
			close(started)
			<-work.Done()
			close(canceled)
			<-joined
			return nil
		})
	}()
	awaitLifecycle(t, started)
	cancel()
	awaitLifecycle(t, canceled)
	select {
	case <-admission.closed:
		t.Error("admission released before owned work joined")
	default:
	}
	close(joined)
	awaitLifecycle(t, done)
	require.NoError(t, result)
	awaitLifecycle(t, admission.closed)
}

func TestLifecycleLossCancelsWorkAndPreservesCause(t *testing.T) {
	t.Parallel()
	admission := &testAdmission{done: make(chan struct{}), closed: make(chan struct{}), err: ErrLost}
	started, done := make(chan struct{}), make(chan struct{})
	var result, cause error
	go func() {
		defer close(done)
		result = runAdmitted(t.Context(), admission, func(work context.Context) error {
			close(started)
			<-work.Done()
			cause = context.Cause(work)
			return work.Err()
		})
	}()
	awaitLifecycle(t, started)
	close(admission.done)
	awaitLifecycle(t, done)
	require.ErrorIs(t, result, ErrLost)
	require.ErrorIs(t, result, context.Canceled)
	require.ErrorIs(t, cause, ErrLost)
	awaitLifecycle(t, admission.closed)
}

func TestLifecycleLossStillSignalsDuringGracefulDrain(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	admission := &testAdmission{done: make(chan struct{}), closed: make(chan struct{}), err: ErrLost}
	started, draining, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var result error
	go func() {
		defer close(done)
		result = runAdmitted(ctx, admission, func(work context.Context) error {
			close(started)
			<-work.Done()
			close(draining)
			<-Lost(work)
			return nil
		})
	}()
	awaitLifecycle(t, started)
	cancel()
	awaitLifecycle(t, draining)
	close(admission.done)
	awaitLifecycle(t, done)
	require.ErrorIs(t, result, ErrLost)
}

func TestLifecycleErrorsAndAcquisitionFailure(t *testing.T) {
	t.Parallel()
	workErr, closeErr := errors.New("work failed"), errors.New("cleanup failed")
	admission := &testAdmission{done: make(chan struct{}), closed: make(chan struct{}), closeErr: closeErr}
	err := runAdmitted(t.Context(), admission, func(context.Context) error { return workErr })
	require.ErrorIs(t, err, workErr)
	require.ErrorIs(t, err, closeErr)
	called := false
	err = Run(t.Context(), nil, 0, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, ErrRole)
	require.False(t, called)
	require.Nil(t, Lost(t.Context()))
}
