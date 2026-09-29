// Package destination resolves mutable Telegram aliases before durable enqueue.
package destination

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"
)

const lookupTimeout = 15 * time.Second
const maxBindingTTL = 5 * time.Minute

var ErrUnavailable = errors.New("destination binding unavailable")

type Resolver interface {
	ResolveChat(context.Context, string) (int64, error)
}

// Bindings is refreshed outside owner transactions. Lookup never performs I/O.
// Queued effects retain their own numeric destination after a refresh.
type Bindings struct {
	mu    sync.RWMutex
	chats map[string]binding
}
type binding struct {
	chat    string
	expires time.Time
}

// Resolve resolves each distinct alias once. The whole operation is bounded.
func Resolve(ctx context.Context, resolver Resolver, chats []string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	result := make(map[string]string, len(chats))
	for _, chat := range chats {
		if _, exists := result[chat]; exists {
			continue
		}
		if numeric, ok := Numeric(chat); ok {
			result[chat] = numeric
			continue
		}
		if resolver == nil || !strings.HasPrefix(chat, "@") {
			return nil, ErrUnavailable
		}
		id, err := resolver.ResolveChat(ctx, chat)
		if err != nil || id == 0 {
			return nil, ErrUnavailable
		}
		result[chat] = strconv.FormatInt(id, 10)
	}
	return result, nil
}

// Refresh updates eligible aliases independently and removes absent aliases.
// It returns an error if any lookup fails, while still publishing successful
// results. Failed aliases retain only their previous, unextended expiry.
func (b *Bindings) Refresh(ctx context.Context, resolver Resolver, chats []string, ttl time.Duration) error {
	if b == nil || ttl <= 0 || ttl > maxBindingTTL {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	b.mu.RLock()
	previous := maps.Clone(b.chats)
	b.mu.RUnlock()
	next := make(map[string]binding, len(chats))
	failed := false
	for result := range refreshAliases(ctx, resolver, chats, ttl) {
		if result.err != nil {
			failed = true
			if old, exists := previous[result.alias]; exists {
				next[result.alias] = old
			}
			continue
		}
		next[result.alias] = result.binding
	}
	b.mu.Lock()
	b.chats = next
	b.mu.Unlock()
	if failed {
		return ErrUnavailable
	}
	return nil
}

type refreshResult struct {
	alias   string
	binding binding
	err     error
}

// Two workers let healthy aliases progress while one lookup waits on its timeout.
// The caller's refresh budget still bounds the complete operation.
func refreshAliases(ctx context.Context, resolver Resolver, chats []string, ttl time.Duration) <-chan refreshResult {
	const workers = 2
	jobs := make(chan string, len(chats))
	results := make(chan refreshResult, len(chats))
	seen := make(map[string]bool, len(chats))
	for _, chat := range chats {
		if !seen[chat] {
			seen[chat] = true
			jobs <- chat
		}
	}
	close(jobs)
	var pending sync.WaitGroup
	for range workers {
		pending.Go(func() {
			for chat := range jobs {
				resolved, err := Resolve(ctx, resolver, []string{chat})
				results <- refreshResult{alias: chat, binding: binding{chat: resolved[chat], expires: time.Now().Add(ttl)}, err: err}
			}
		})
	}
	go func() {
		pending.Wait()
		close(results)
	}()
	return results
}
func (b *Bindings) Lookup(chat string) (string, error) {
	if numeric, ok := Numeric(chat); ok {
		return numeric, nil
	}
	if b == nil {
		return "", ErrUnavailable
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	entry, exists := b.chats[chat]
	if !exists || !time.Now().Before(entry.expires) {
		return "", ErrUnavailable
	}
	return entry.chat, nil
}

func Numeric(chat string) (string, bool) {
	id, err := strconv.ParseInt(chat, 10, 64)
	if err != nil || id == 0 {
		return "", false
	}
	return strconv.FormatInt(id, 10), true
}
