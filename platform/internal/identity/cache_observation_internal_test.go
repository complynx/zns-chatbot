package identity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCacheObservationDoesNotRenewEvictOrExposeCredentials(t *testing.T) {
	t.Parallel()
	now := time.Now()
	adapter := &Zitadel{}
	adapter.exchanges.values = map[string]cachedIdentity{
		"private-subject": {value: "secret-token", subject: "private-subject", until: now.Add(time.Hour)},
		"expired-subject": {value: "expired-secret", until: now.Add(-time.Hour)},
	}
	adapter.exchanges.flights = map[string]*identityFlight{"private-flight": {invalidated: true}}
	adapter.verified.values = map[string]cachedIdentity{
		"token-hash": {value: "private-subject", until: now.Add(time.Hour)},
	}
	before := adapter.exchanges.values["private-subject"]
	for range 2 {
		item, err := adapter.CacheObservations(t.Context())
		require.NoError(t, err)
		require.Equal(t, CacheObservations{Exchange: CacheObservation{Usable: 1, Expired: 1, Flights: 1},
			Introspection: CacheObservation{Usable: 1}, Capacity: identityCacheLimit}, item)
	}
	require.Equal(t, before, adapter.exchanges.values["private-subject"])
	require.Len(t, adapter.exchanges.values, 2, "scraping must retain expired entries")
	require.True(t, adapter.exchanges.flights["private-flight"].invalidated)
	adapter.InvalidateSubject("private-subject")
	item, err := adapter.CacheObservations(t.Context())
	require.NoError(t, err)
	require.Zero(t, item.Exchange.Usable, "committed invalidation remains visible")
}

func TestCacheObservationContentionCancellationAndRecovery(t *testing.T) {
	t.Parallel()
	adapter := &Zitadel{}
	for _, cache := range []*positiveIdentityCache{&adapter.exchanges, &adapter.verified} {
		cache.mu.Lock()
		item, err := adapter.CacheObservations(t.Context())
		cache.mu.Unlock()
		require.ErrorIs(t, err, errCacheObservationUnavailable)
		require.Equal(t, CacheObservations{}, item)
		require.True(t, adapter.exchanges.mu.TryLock(), "partial snapshot must release first cache")
		adapter.exchanges.mu.Unlock()
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := adapter.CacheObservations(ctx)
	require.ErrorIs(t, err, context.Canceled)
	item, err := adapter.CacheObservations(t.Context())
	require.NoError(t, err)
	require.Equal(t, identityCacheLimit, item.Capacity)
	var missing *Zitadel
	_, err = missing.CacheObservations(t.Context())
	require.ErrorIs(t, err, errCacheObservationUnavailable)
}
