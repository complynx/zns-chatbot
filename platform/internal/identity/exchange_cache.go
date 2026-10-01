package identity

import (
	"context"
	"sync"
	"time"
)

const identityCacheLimit = 1024
const identityLookupTimeout = 10 * time.Second

type cachedIdentity struct {
	value   string
	subject string
	until   time.Time
}

type identityFlight struct {
	done        chan struct{}
	value       cachedIdentity
	err         error
	generation  uint64
	invalidated bool
}

// positiveIdentityCache bounds both stored credentials and concurrent lookups.
// Hits never extend deadlines. Provider work has its own bounded lifetime so a
// cancelled waiter cannot cancel another caller's shared lookup.
type positiveIdentityCache struct {
	mu         sync.Mutex
	values     map[string]cachedIdentity
	flights    map[string]*identityFlight
	generation uint64
}

func (c *positiveIdentityCache) resolve(
	ctx context.Context,
	key string,
	load func(context.Context) (cachedIdentity, error),
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	if entry, ok := c.values[key]; ok && time.Now().Before(entry.until) {
		c.mu.Unlock()
		return entry.value, nil
	}
	delete(c.values, key)
	flight := c.flights[key]
	if flight == nil {
		if !c.makeRoom() {
			c.mu.Unlock()
			return "", ErrZitadelUnavailable
		}
		if c.flights == nil {
			c.flights = make(map[string]*identityFlight)
		}
		flight = &identityFlight{done: make(chan struct{}), generation: c.generation}
		c.flights[key] = flight
		go c.load(context.WithoutCancel(ctx), key, flight, load)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-flight.done:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if flight.err != nil {
			return "", flight.err
		}
		c.mu.Lock()
		retired := flight.invalidated || flight.generation != c.generation
		c.mu.Unlock()
		if retired {
			return "", ErrZitadelIdentity
		}
		if !time.Now().Before(flight.value.until) {
			return "", ErrZitadelIdentity
		}
		return flight.value.value, nil
	}
}

// makeRoom runs under mu and never evicts in-flight work.
func (c *positiveIdentityCache) makeRoom() bool {
	now := time.Now()
	for key, entry := range c.values {
		if !now.Before(entry.until) {
			delete(c.values, key)
		}
	}
	if len(c.values)+len(c.flights) < identityCacheLimit {
		return true
	}
	var oldest string
	var deadline time.Time
	for key, entry := range c.values {
		if deadline.IsZero() || entry.until.Before(deadline) {
			oldest, deadline = key, entry.until
		}
	}
	if deadline.IsZero() {
		return false
	}
	delete(c.values, oldest)
	return true
}

func (c *positiveIdentityCache) load(
	ctx context.Context,
	key string,
	flight *identityFlight,
	load func(context.Context) (cachedIdentity, error),
) {
	ctx, cancel := context.WithTimeout(ctx, identityLookupTimeout)
	defer cancel()
	entry, err := load(ctx)
	if err == nil && (entry.value == "" || !time.Now().Before(entry.until)) {
		err = ErrZitadelIdentity
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.flights, key)
	if err == nil && (flight.invalidated || flight.generation != c.generation) {
		err = ErrZitadelIdentity
	}
	if err == nil {
		if c.values == nil {
			c.values = make(map[string]cachedIdentity)
		}
		c.values[key] = entry
	}
	flight.value, flight.err = entry, err
	close(flight.done)
}

// invalidate preserves other completed subjects. Unresolved introspections have
// no trusted subject yet, so their generation is conservatively retired together.
func (c *positiveIdentityCache) invalidate(subject string, unresolvedSubjects bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.values {
		if entry.subject == subject {
			delete(c.values, key)
		}
	}
	if unresolvedSubjects {
		c.generation++
	} else if flight := c.flights[subject]; flight != nil {
		flight.invalidated = true
	}
}

func (c *positiveIdentityCache) subject(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[key].subject
}

// subjectsForValue uses associations established by successful exchanges, never
// claims from a rejected token. Callers invalidate after this lock is released.
func (c *positiveIdentityCache) subjectsForValue(value string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var subjects []string
	for _, entry := range c.values {
		if entry.value == value {
			subjects = append(subjects, entry.subject)
		}
	}
	return subjects
}

// InvalidateSubject retires both credential caches after a committed local
// identity or permission change. Callers must supply the stored Zitadel subject.
func (z *Zitadel) InvalidateSubject(subject string) {
	if subject == "" {
		return
	}
	z.exchanges.invalidate(subject, false)
	z.verified.invalidate(subject, true)
}
