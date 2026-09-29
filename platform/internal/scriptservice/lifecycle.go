package scriptservice

import (
	"context"
	"sync"
)

type session struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type lifecycle struct {
	mu       sync.Mutex
	stopping bool
	active   *session
}

func (l *lifecycle) begin(parent context.Context) (context.Context, func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stopping || l.active != nil {
		return parent, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	current := &session{cancel: cancel, done: make(chan struct{})}
	l.active = current
	return ctx, func() {
		cancel()
		l.mu.Lock()
		defer l.mu.Unlock()
		l.active = nil
		close(current.done)
	}, true
}

func (l *lifecycle) stop() <-chan struct{} {
	l.mu.Lock()
	l.stopping = true
	current := l.active
	l.mu.Unlock()
	if current == nil {
		return nil
	}
	current.cancel()
	return current.done
}

// Stop permanently closes session admission and cancels the admitted session.
// The HTTP owner must also close its server to unblock incomplete request bodies.
func (s *Service) Stop() {
	s.sessions.stop()
}

// Shutdown cancels work and waits for its handler, child and relay to finish.
// A deadline error means work has not joined; it is not a completion receipt.
func (s *Service) Shutdown(ctx context.Context) error {
	done := s.sessions.stop()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
