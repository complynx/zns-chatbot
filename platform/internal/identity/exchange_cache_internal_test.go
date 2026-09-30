package identity

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdentityCacheCoalescesAndCancelsWaiters(t *testing.T) {
	t.Parallel()
	var cache positiveIdentityCache
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	load := func(ctx context.Context) (cachedIdentity, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return cachedIdentity{value: "token", subject: "alice", until: time.Now().Add(time.Minute)}, nil
		case <-ctx.Done():
			return cachedIdentity{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := cache.resolve(ctx, "alice", load); first <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	var workers sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		workers.Go(func() { _, err := cache.resolve(t.Context(), "alice", load); results <- err })
	}
	other, err := cache.resolve(t.Context(), "bob", func(context.Context) (cachedIdentity, error) {
		return cachedIdentity{value: "other", subject: "bob", until: time.Now().Add(time.Minute)}, nil
	})
	require.NoError(t, err, "another subject must not wait for Alice's network request")
	require.Equal(t, "other", other)
	close(release)
	workers.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result)
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestIdentityCacheExpiryAndErrorsNeverExtendAccess(t *testing.T) {
	t.Parallel()
	var cache positiveIdentityCache
	var calls int
	failed := true
	deadline := time.Now().Add(time.Minute)
	load := func(context.Context) (cachedIdentity, error) {
		calls++
		if failed {
			return cachedIdentity{}, ErrZitadelUnavailable
		}
		return cachedIdentity{value: "token", subject: "alice", until: deadline}, nil
	}
	_, err := cache.resolve(t.Context(), "alice", load)
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	failed = false
	_, err = cache.resolve(t.Context(), "alice", load)
	require.NoError(t, err)
	failed = true
	_, err = cache.resolve(t.Context(), "alice", load)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, deadline, cache.values["alice"].until)
	cache.values["alice"] = cachedIdentity{value: "token", subject: "alice", until: time.Now().Add(-time.Second)}
	value, err := cache.resolve(t.Context(), "alice", load)
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	require.Empty(t, value)
	require.Empty(t, cache.values)
	require.Equal(t, 3, calls)
}

func TestIdentityCacheInvalidationRetiresInflightResults(t *testing.T) {
	t.Parallel()
	for _, unresolved := range []bool{false, true} {
		t.Run(strconv.FormatBool(unresolved), func(t *testing.T) {
			t.Parallel()
			var cache positiveIdentityCache
			cache.values = map[string]cachedIdentity{
				"bob": {value: "other", subject: "bob", until: time.Now().Add(time.Minute)},
			}
			started, release := make(chan struct{}), make(chan struct{})
			result := make(chan error, 1)
			go func() {
				_, err := cache.resolve(t.Context(), "alice", func(context.Context) (cachedIdentity, error) {
					close(started)
					<-release
					return cachedIdentity{value: "token", subject: "alice", until: time.Now().Add(time.Minute)}, nil
				})
				result <- err
			}()
			<-started
			cache.invalidate("alice", unresolved)
			close(release)
			require.ErrorIs(t, <-result, ErrZitadelIdentity)
			require.NotContains(t, cache.values, "alice")
			require.Contains(t, cache.values, "bob")
		})
	}
}

func TestIdentityCacheBoundsStoredAndPendingEntries(t *testing.T) {
	t.Parallel()
	var cache positiveIdentityCache
	for index := range identityCacheLimit + 1 {
		subject := strconv.Itoa(index)
		_, err := cache.resolve(t.Context(), subject, func(context.Context) (cachedIdentity, error) {
			return cachedIdentity{value: "token", subject: subject, until: time.Now().Add(time.Minute)}, nil
		})
		require.NoError(t, err)
	}
	require.Len(t, cache.values, identityCacheLimit)
	cache.values = nil
	cache.flights = make(map[string]*identityFlight)
	for index := range identityCacheLimit {
		cache.flights[strconv.Itoa(index)] = &identityFlight{done: make(chan struct{})}
	}
	_, err := cache.resolve(t.Context(), "overflow", func(context.Context) (cachedIdentity, error) {
		return cachedIdentity{}, errors.New("unexpected provider request")
	})
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	require.Len(t, cache.flights, identityCacheLimit)
}
