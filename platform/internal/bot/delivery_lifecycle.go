package bot

import (
	"context"
	"time"
)

// A completed wire attempt must record its outcome even if shutdown cancelled
// the caller. The serial worker still joins this bounded persistence operation.
func deliveryCompletionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
}

// startBotDelivery runs one delivery pass at a time, independently of ingress.
// The caller must stop and join it before releasing the bot ownership lock.
func startBotDelivery(parent context.Context, deliver func(context.Context)) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for ctx.Err() == nil {
			deliver(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
