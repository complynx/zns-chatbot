package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

// Core workers run only in API/app mode. Each failed startup unwinds workers
// already started; normal shutdown uses the same reverse order. onFatal
// receives only a safe positive database failure observed after startup.
func startProductMaintenance(
	ctx context.Context,
	db *pgxpool.Pool,
	logger *slog.Logger,
	cfg config.Config,
	services appservices.Services,
	runtime *observability.Runtime,
	onFatal func(error),
) (func(), error) {
	// Observe supplied owner state without enabling sources or requiring a new service.
	if runtime != nil && services.Knowledge.DB != nil {
		if err := runtime.RegisterAssistantSources(services.Knowledge); err != nil {
			return nil, err
		}
	}
	stopMaintenance, err := startMaintenance(ctx, services, logger, cfg.Orders.ReminderAfter, onFatal)
	if err != nil {
		return nil, err
	}
	stopSources, err := startAssistantSources(ctx, db, logger, cfg, onFatal)
	if err != nil {
		stopMaintenance()
		return nil, err
	}
	stopFood, err := startFoodMaintenance(ctx, services.LegacyFood, logger, onFatal)
	if err != nil {
		stopSources()
		stopMaintenance()
		return nil, err
	}
	return func() { stopFood(); stopSources(); stopMaintenance() }, nil
}

// databaseFatal returns the safe marker only for a positively classified
// database failure. Provider and domain errors stay on the caller's path.
func databaseFatal(err error) error {
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	return nil
}

// startTicks runs tick at interval in one joined goroutine. tick handles
// ordinary failures itself; a returned error is a database failure, reported
// once through onFatal, after which no later tick starts.
func startTicks(
	ctx context.Context,
	interval time.Duration,
	tick func(context.Context) error,
	onFatal func(error),
) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := tick(ctx); err != nil {
					onFatal(err)
					return
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

// fatalLatch retains the first background database failure independently of
// the context cause: it records before canceling the owner's work context, so
// an earlier ordinary cancellation or a nil cleanup result cannot hide it.
type fatalLatch struct {
	first  chan error
	cancel context.CancelFunc
}

func newFatalLatch(cancel context.CancelFunc) fatalLatch {
	return fatalLatch{first: make(chan error, 1), cancel: cancel}
}

func (l fatalLatch) report(err error) {
	if !core.IsDatabaseFailure(err) {
		return
	}
	select {
	case l.first <- core.ErrDatabase:
	default:
	}
	l.cancel()
}

// result joins the retained failure with the owner's result. Call it only
// after every worker reporting to this latch has joined.
func (l fatalLatch) result(err error) error {
	select {
	case fatal := <-l.first:
		return errors.Join(fatal, err)
	default:
		return err
	}
}
