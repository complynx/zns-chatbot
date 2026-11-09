package task

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

const menuRetryInterval = 30 * time.Second
const menuRejectedRetryInterval = time.Hour

type menuRetryState struct {
	key   string
	until time.Time
}
type menuOperationError struct {
	operation string
	cause     error
}

func (e *menuOperationError) Error() string { return "menu operation failed" }
func (e *menuOperationError) Unwrap() error { return e.cause }

// Caller holds menuMu. Failed applications never become successful cache entries.
func (s *Service) recordMenuFailure(chatID int64, key string, err error) {
	delete(s.menuApplied, chatID)
	delay := menuRetryInterval
	if failure, ok := errors.AsType[*telegram.Error](err); ok {
		if failure.StatusCode == http.StatusBadRequest || failure.StatusCode == http.StatusForbidden {
			delay = menuRejectedRetryInterval
		}
		if failure.StatusCode == http.StatusTooManyRequests {
			delay = max(delay, failure.RetryAfter)
			s.menuPausedUntil = maxMenuDeadline(s.menuPausedUntil, time.Now().Add(delay))
		}
	}
	if s.menuRetry == nil {
		s.menuRetry = make(map[int64]menuRetryState)
	}
	s.menuRetry[chatID] = menuRetryState{key: key, until: time.Now().Add(delay)}
}

func maxMenuDeadline(previous, next time.Time) time.Time {
	if next.After(previous) {
		return next
	}
	return previous
}

// A fresh user interaction may retry its chat, but never bypasses Telegram's quota.
func (s *Service) resetMenuRetry(chatID int64) {
	if !s.menuMu.TryLock() {
		return
	}
	defer s.menuMu.Unlock()
	delete(s.menuRetry, chatID)
}

func (s *Service) menuPauseDelay() time.Duration {
	s.menuMu.Lock()
	defer s.menuMu.Unlock()
	return max(time.Duration(0), time.Until(s.menuPausedUntil))
}

func (s *Service) recordMenuRateLimit(err error) {
	var failure *telegram.Error
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusTooManyRequests {
		return
	}
	s.menuMu.Lock()
	defer s.menuMu.Unlock()
	s.menuPausedUntil = maxMenuDeadline(s.menuPausedUntil, time.Now().Add(max(menuRetryInterval, failure.RetryAfter)))
}

// Never log raw errors, recipient IDs, requests, tokens or Telegram descriptions.
func (s *Service) warnMenuFailure(ctx context.Context, err error) {
	if s.logger == nil || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	operation := "availability_read"
	if step, ok := errors.AsType[*menuOperationError](err); ok {
		operation = step.operation
	}
	fields := []any{"class", "telegram_menu", "operation", operation}
	if failure, ok := errors.AsType[*telegram.Error](err); ok {
		fields = append(fields, "status", failure.StatusCode, "outcome", string(failure.Outcome),
			"reason", failure.ReasonClass(), "retryable", failure.Retryable,
			"retry_after_seconds", failure.RetryAfter.Seconds())
	}
	fields = append(fields, "deadline_exceeded", errors.Is(ctx.Err(), context.DeadlineExceeded))
	s.logger.WarnContext(ctx, "Telegram menu refresh unavailable", fields...)
}
