package bot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeliveryWorkerStopJoinsActivePass(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	stop := startBotDelivery(t.Context(), func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil
	}, func(error) { t.Error("unexpected fatal delivery result") })
	t.Cleanup(stop)
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("delivery did not start")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("delivery did not receive cancellation")
	}
	select {
	case <-stopped:
		t.Error("stop returned before the active delivery finished")
	default:
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not join completed delivery")
	}
}
func TestDeliveryWorkerCancelledParentDoesNotDeliver(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := make(chan struct{}, 1)
	stop := startBotDelivery(ctx, func(context.Context) error {
		calls <- struct{}{}
		return nil
	},
		func(error) { t.Error("unexpected fatal delivery result") })
	stop()
	require.Empty(t, calls)
}

func TestDeliveryWorkerWaitsAfterSlowPass(t *testing.T) {
	t.Parallel()
	first := true
	completed := make(chan time.Time, 1)
	next := make(chan time.Time, 1)
	stop := startBotDelivery(t.Context(), func(ctx context.Context) error {
		if first {
			first = false
			timer := time.NewTimer(pollInterval + 20*time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil
			case <-timer.C:
			}
			completed <- time.Now()
			return nil
		}
		next <- time.Now()
		<-ctx.Done()
		return nil
	}, func(error) { t.Error("unexpected fatal delivery result") })
	t.Cleanup(stop)
	var finished time.Time
	select {
	case finished = <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery did not complete")
	}
	select {
	case started := <-next:
		require.GreaterOrEqual(t, started.Sub(finished), pollInterval)
	case <-time.After(2 * time.Second):
		t.Fatal("next delivery did not start")
	}
}

func TestDeliveryCompletionOutlivesCancelledParentWithinDeadline(t *testing.T) {
	t.Parallel()
	parent, stop := context.WithCancel(t.Context())
	stop()
	started := time.Now()
	completion, finish := deliveryCompletionContext(parent)
	defer finish()
	require.NoError(t, completion.Err())
	deadline, bounded := completion.Deadline()
	require.True(t, bounded)
	require.WithinDuration(t, started.Add(unlockTimeout), deadline, time.Second)
	finish()
	require.ErrorIs(t, completion.Err(), context.Canceled)
}
