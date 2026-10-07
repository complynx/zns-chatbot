package runtimeapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestCancellationDoesNotStartOwnerShutdown(t *testing.T) {
	t.Parallel()
	owner, finish := WithShutdownBudget(t.Context(), 20*time.Millisecond)
	defer finish()
	request, cancel := context.WithCancel(owner)
	cancel()
	completion, done := CompletionContext(request, 50*time.Millisecond)
	defer done()
	require.NoError(t, completion.Err(), "cancelled requests retain their local receipt budget")
	<-completion.Done()
	require.NoError(t, ShutdownError(owner), "healthy owner must not acquire a stop deadline")
}

func TestOwnerStopCutsOffExistingCompletionAndLaterPhases(t *testing.T) {
	t.Parallel()
	owner, finish := WithShutdownBudget(t.Context(), 30*time.Millisecond)
	defer finish()
	type authorityKey struct{}
	request := context.WithValue(owner, authorityKey{}, "request-authority")
	completion, done := CompletionContext(request, time.Second)
	defer done()
	BeginShutdown(owner)
	<-completion.Done()
	require.ErrorIs(t, context.Cause(completion), context.DeadlineExceeded)
	require.Equal(t, "request-authority", completion.Value(authorityKey{}))
	later, stop := CompletionContext(owner, time.Second)
	defer stop()
	require.ErrorIs(t, later.Err(), context.DeadlineExceeded)
	require.ErrorIs(t, ShutdownError(owner), context.DeadlineExceeded)
}

func TestShutdownDoesNotRenewAcrossSequentialPhases(t *testing.T) {
	t.Parallel()
	owner, finish := WithShutdownBudget(t.Context(), time.Second)
	defer finish()
	BeginShutdown(owner)
	drain, stopDrain := CompletionContext(owner, time.Second)
	defer stopDrain()
	flush, stopFlush := CompletionContext(owner, time.Second)
	defer stopFlush()
	first, ok := drain.Deadline()
	require.True(t, ok)
	second, ok := flush.Deadline()
	require.True(t, ok)
	require.Equal(t, first, second)
	require.NoError(t, drain.Err())
	require.NoError(t, flush.Err())
}

func TestNormalShutdownHardLimit(t *testing.T) {
	t.Parallel()
	owner, finish := WithShutdownBudget(t.Context(), 10*time.Second)
	defer finish()
	BeginShutdown(owner)
	cleanup, stop := CompletionContext(owner, 10*time.Second)
	defer stop()
	deadline, ok := cleanup.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
}
