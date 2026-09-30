package identity

import (
	"context"
	"errors"
	"time"
)

// CacheObservation contains counts only. Expired entries may remain stored until
// the next lookup; they are not usable credentials. Flights include retired work
// until its bounded provider call completes.
type CacheObservation struct {
	Usable  int
	Expired int
	Flights int
}

type CacheObservations struct {
	Exchange      CacheObservation
	Introspection CacheObservation
	Capacity      int
}

var errCacheObservationUnavailable = errors.New("identity cache observation unavailable")

// CacheObservations reads both bounded caches without refreshing, evicting or
// waiting for authentication work. Contention is an unavailable snapshot.
func (z *Zitadel) CacheObservations(ctx context.Context) (CacheObservations, error) {
	if err := ctx.Err(); err != nil {
		return CacheObservations{}, err
	}
	if z == nil || !z.exchanges.mu.TryLock() {
		return CacheObservations{}, errCacheObservationUnavailable
	}
	defer z.exchanges.mu.Unlock()
	if !z.verified.mu.TryLock() {
		return CacheObservations{}, errCacheObservationUnavailable
	}
	defer z.verified.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CacheObservations{}, err
	}
	now := time.Now()
	return CacheObservations{
		Exchange:      z.exchanges.observe(now),
		Introspection: z.verified.observe(now),
		Capacity:      identityCacheLimit,
	}, nil
}

// observe runs under the cache lock and never returns keys or credential data.
func (c *positiveIdentityCache) observe(now time.Time) CacheObservation {
	result := CacheObservation{Flights: len(c.flights)}
	for _, entry := range c.values {
		if now.Before(entry.until) {
			result.Usable++
		} else {
			result.Expired++
		}
	}
	return result
}
