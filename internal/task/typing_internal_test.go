package task

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTypingRefreshStopsAfterDeliveryAndContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	checked := make(chan struct{}, 10)
	sent := make(chan int64, 10)
	var active atomic.Bool
	active.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runTyping(ctx, ticks, func(context.Context, int64) ([]int64, error) {
			checked <- struct{}{}
			return []int64{42}, nil
		}, func(context.Context, int64) (bool, error) { return active.Load(), nil },
			func(ctx context.Context, id int64) error {
				_, bounded := ctx.Deadline()
				if !bounded {
					panic("unbounded typing request")
				}
				sent <- id
				return errors.New("temporary action failure")
			})
	}()
	now := time.Now()
	ticks <- now
	<-checked
	require.EqualValues(t, 42, <-sent)
	// Failures get the same throttle; they do not cause API retries.
	ticks <- now.Add(time.Second)
	<-checked
	ticks <- now.Add(typingRefresh)
	<-checked
	require.EqualValues(t, 42, <-sent)
	active.Store(false)
	ticks <- now.Add(2 * typingRefresh)
	<-checked
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("typing did not stop")
	}
	require.Empty(t, sent)
}

func TestTypingRefresh128ChatsDoesNotWaitForDiscoveryWrap(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	var tick atomic.Int64
	var mu sync.Mutex
	seen := map[int64][]int64{}
	sent := make(chan struct{}, 512)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runTyping(ctx, ticks, func(_ context.Context, after int64) ([]int64, error) {
			var ids []int64
			for id := after + 1; id <= 128 && len(ids) < 64; id++ {
				ids = append(ids, id)
			}
			return ids, nil
		}, func(context.Context, int64) (bool, error) { return true, nil }, func(_ context.Context, id int64) error {
			mu.Lock()
			seen[id] = append(seen[id], tick.Load())
			mu.Unlock()
			sent <- struct{}{}
			return nil
		})
	}()
	count := 0
	for second := range int64(10) {
		tick.Store(second)
		ticks <- time.Unix(1000+second, 0)
		want := 64 * (1 + int(second)/4)
		if second >= 1 {
			want += 64 * (1 + int(second-1)/4)
		}
		for count < want {
			select {
			case <-sent:
				count++
			case <-time.After(time.Second):
				t.Fatal("typing page stalled")
			}
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("typing did not stop")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 128)
	for _, times := range seen {
		require.Len(t, times, 3)
		for i := range len(times) - 1 {
			require.EqualValues(t, 4, times[i+1]-times[i])
		}
	}
}

func TestTypingSlowActionsAreBoundedAndCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	started := make(chan struct{}, typingConcurrency)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runTyping(ctx, ticks, func(context.Context, int64) ([]int64, error) {
			ids := make([]int64, 64)
			for i := range ids {
				ids[i] = int64(i + 1)
			}
			return ids, nil
		}, func(context.Context, int64) (bool, error) { return true, nil }, func(ctx context.Context, _ int64) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	ticks <- time.Now()
	for range typingConcurrency {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("actions serialized")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight actions survived cancellation")
	}
	require.Empty(t, started)
}
