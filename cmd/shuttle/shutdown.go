package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const drainCompleted = "completed"
const drainUnfinished = "unfinished"

type drainSummary struct {
	Duration                                  time.Duration
	Outcome, PublicHTTP, PrivateHTTP, Workers string
}

type drainResult struct {
	component string
	err       error
}

// drain observes the three existing shutdown callbacks, not deferred process cleanup.
// An unfinished result means its acknowledgement was not observed before return.
func drain(
	ctx context.Context,
	stopWorkers func(context.Context) error,
	public, private *http.Server,
) drainSummary {
	started := time.Now()
	var summary drainSummary
	summary.PublicHTTP, summary.PrivateHTTP, summary.Workers = drainUnfinished, drainUnfinished, drainUnfinished
	finish := func() drainSummary {
		summary.Duration = time.Since(started)
		summary.Outcome = drainOutcome(ctx.Err(), summary)
		return summary
	}
	done := make(chan drainResult, shutdownParticipants)
	go func() { done <- drainResult{"public", stopHTTP(ctx, public)} }()
	go func() { done <- drainResult{"private", stopHTTP(ctx, private)} }()
	go func() { done <- drainResult{"workers", stopWorkers(ctx)} }()
	record := func(result drainResult) {
		status := drainStatus(result.err)
		switch result.component {
		case "public":
			summary.PublicHTTP = status
		case "private":
			summary.PrivateHTTP = status
		case "workers":
			summary.Workers = status
		}
	}
	for range shutdownParticipants {
		select {
		case result := <-done:
			record(result)
		case <-ctx.Done():
			_ = public.Close()
			_ = private.Close()
			// Include acknowledgements already available when cancellation was observed.
			for {
				select {
				case result := <-done:
					record(result)
				default:
					return finish()
				}
			}
		}
	}
	return finish()
}

func stopHTTP(ctx context.Context, server *http.Server) error {
	err := server.Shutdown(ctx)
	closeErr := server.Close()
	if errors.Is(closeErr, net.ErrClosed) || errors.Is(closeErr, http.ErrServerClosed) {
		closeErr = nil
	}
	return errors.Join(err, closeErr)
}

func drainStatus(err error) string {
	switch {
	case err == nil:
		return drainCompleted
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "error"
	}
}

func drainOutcome(err error, summary drainSummary) string {
	contextStatus := drainStatus(err)
	for _, status := range []string{"deadline", "cancelled", "error"} {
		if status == contextStatus || status == summary.PublicHTTP || status == summary.PrivateHTTP ||
			status == summary.Workers {
			return status
		}
	}
	return drainCompleted
}

func logDrain(ctx context.Context, logger *slog.Logger, summary drainSummary, budget time.Duration) {
	level := slog.LevelInfo
	if summary.Outcome != drainCompleted {
		level = slog.LevelWarn
	}
	logger.Log(ctx, level, "application drain finished",
		"outcome", summary.Outcome, "duration_ms", float64(summary.Duration)/float64(time.Millisecond),
		"budget_ms", float64(budget)/float64(time.Millisecond),
		"public_http", summary.PublicHTTP, "private_http", summary.PrivateHTTP, "workers", summary.Workers)
}
