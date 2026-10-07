package bot

import (
	"context"
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// A completed wire attempt must record its outcome even if shutdown cancelled
// the caller. The serial worker still joins this bounded persistence operation.
func deliveryCompletionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
}

// startBotDelivery runs one delivery pass at a time, independently of ingress.
// The caller must stop and join it before releasing the bot ownership lock.
func startBotDelivery(parent context.Context, deliver func(context.Context) error, onFatal func(error)) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			if err := deliver(ctx); core.IsDatabaseFailure(err) {
				onFatal(core.ErrDatabase)
				return
			}
			// Wait after completion so slow recovery passes cannot consume a
			// pending ticker event and run continuously alongside ingress.
			timer := time.NewTimer(pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// Join before reading the fatal result: bounded receipt completion may still fail
// after ordinary shutdown begins. The caller retains bot ownership until return.
func finishBotDelivery(stop func(), fatal <-chan error, result error) error {
	stop()
	select {
	case failure := <-fatal:
		return errors.Join(result, failure)
	default:
		return result
	}
}
