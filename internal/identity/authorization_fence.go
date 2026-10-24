package identity

// InvalidationFence captures local invalidation generations before verification.
// The returned check does no I/O and can fence an in-flight database commit.
// It does not discover remote revocation; normal bounded IAM caching still applies.
func (c *Client) InvalidationFence(subject string) func() bool {
	c.mu.Lock()
	generation, epoch := c.generation[subject], c.cacheEpoch
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.generation[subject] == generation && c.cacheEpoch == epoch
	}
}
