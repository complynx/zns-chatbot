package identity

import "time"

// CacheStats contains cumulative decisions for this client, without identity data.
// Positive lookups follow JWT trust validation. Fallbacks count successful
// singleflight decisions, rather than the callers sharing their result.
type CacheStats struct {
	PositiveHits, PositiveMisses    int64
	ServiceFallbacks, UserFallbacks int64
}

// CacheStats reads local counters without contacting IAM or changing cache state.
func (c *Client) CacheStats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cacheStats
}

// positiveLookup checks validity and records the decision under the client lock.
func (c *Client) positiveLookup(hash [32]byte) (positive, bool) {
	cached, ok := c.positive[hash]
	hit := ok && time.Now().Before(cached.until)
	if hit {
		c.cacheStats.PositiveHits++
	} else {
		c.cacheStats.PositiveMisses++
	}
	return cached, hit
}

// recordFallback runs under the client lock after the invalidation fence passes.
func (c *Client) recordFallback(subject string) {
	if subject == c.options.ActorID {
		c.cacheStats.ServiceFallbacks++
	} else {
		c.cacheStats.UserFallbacks++
	}
}
