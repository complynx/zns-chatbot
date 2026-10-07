package runtimeapp

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type runtimeAdmission interface {
	Done() <-chan struct{}
	Err() error
	Close(context.Context) error
}

type lossKey struct{}

// Lost closes when this runtime loses admission, including during graceful
// shutdown. It is nil outside Run. It is not a transaction or effect fence.
func Lost(ctx context.Context) <-chan struct{} {
	lost, _ := ctx.Value(lossKey{}).(<-chan struct{})
	return lost
}

// Run acquires ownership before constructing the runtime. Work must stop ingress
// and join its workers before returning; ownership is released only afterward.
// Loss cancels work and is returned without attempting to reacquire admission.
func Run(ctx context.Context, config *pgx.ConnConfig, role Role, work func(context.Context) error) error {
	admission, err := Acquire(ctx, config, role)
	if err != nil {
		return err
	}
	return runAdmitted(ctx, admission, work)
}

func runAdmitted(ctx context.Context, admission runtimeAdmission, work func(context.Context) error) (result error) {
	lost := make(chan struct{})
	owned, cancel := context.WithCancelCause(context.WithValue(ctx, lossKey{}, (<-chan struct{})(lost)))
	finished, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-admission.Done():
			BeginShutdown(ctx)
			close(lost)
			cancel(errors.Join(ErrLost, admission.Err()))
		case <-finished:
		}
	}()
	defer func() {
		BeginShutdown(ctx)
		cancel(context.Canceled)
		close(finished)
		<-watched
		cleanup, stop := CompletionContext(ctx, cleanupTimeout)
		defer stop()
		result = errors.Join(result, admission.Close(cleanup), ShutdownError(ctx))
	}()
	if err := owned.Err(); err != nil {
		return err
	}
	select {
	case <-admission.Done():
		return errors.Join(ErrLost, admission.Err())
	default:
		return work(owned)
	}
}
