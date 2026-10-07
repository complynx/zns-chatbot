package runtimeapp

import (
	"context"
	"sync"
	"time"
)

const normalShutdownLimit = 5 * time.Second

type shutdownKey struct{}

type shutdownBudget struct {
	mu       sync.Mutex
	limit    time.Duration
	deadline time.Time
	closed   bool
	timer    *time.Timer
	cutoff   context.Context
	cancel   context.CancelCauseFunc
}

// WithShutdownBudget shares one normal-stop window without timing live work.
func WithShutdownBudget(ctx context.Context, limit time.Duration) (context.Context, func()) {
	if _, ok := ctx.Value(shutdownKey{}).(*shutdownBudget); ok {
		return ctx, func() {}
	}
	if limit <= 0 || limit > normalShutdownLimit {
		limit = normalShutdownLimit
	}
	cutoff, cancel := context.WithCancelCause(context.Background())
	budget := &shutdownBudget{limit: limit, cutoff: cutoff, cancel: cancel}
	ctx = context.WithValue(ctx, shutdownKey{}, budget)
	stop := context.AfterFunc(ctx, func() { BeginShutdown(ctx) })
	return ctx, func() {
		stop()
		budget.mu.Lock()
		budget.closed = true
		if budget.timer != nil {
			budget.timer.Stop()
		}
		budget.mu.Unlock()
		cancel(context.Canceled)
	}
}

// BeginShutdown is for owner stop or final cleanup, not request cancellation.
func BeginShutdown(ctx context.Context) {
	budget, ok := ctx.Value(shutdownKey{}).(*shutdownBudget)
	if !ok {
		return
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.closed || !budget.deadline.IsZero() {
		return
	}
	budget.deadline = time.Now().Add(budget.limit)
	budget.timer = time.AfterFunc(budget.limit, func() { budget.cancel(context.DeadlineExceeded) })
}

// CompletionContext retains request values and a local receipt budget. A later
// owner stop also cuts off already-started completions at its shared deadline.
func CompletionContext(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	base, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	deadline := time.Now().Add(limit)
	stop := func() bool { return false }
	budget, ok := ctx.Value(shutdownKey{}).(*shutdownBudget)
	if ok {
		stop = context.AfterFunc(budget.cutoff, func() { cancel(context.Cause(budget.cutoff)) })
		budget.mu.Lock()
		if !budget.deadline.IsZero() && budget.deadline.Before(deadline) {
			deadline = budget.deadline
		}
		budget.mu.Unlock()
	}
	bounded, finish := context.WithDeadline(base, deadline)
	return bounded, func() {
		stop()
		finish()
		cancel(context.Canceled)
	}
}

// ShutdownError prevents late cleanup from being reported as normal success.
func ShutdownError(ctx context.Context) error {
	budget, ok := ctx.Value(shutdownKey{}).(*shutdownBudget)
	if !ok {
		return nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if !budget.deadline.IsZero() && !time.Now().Before(budget.deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
