package descriptor

import (
	"context"
	"sync"
	"time"

	"github.com/complynx/zns-chatbot/internal/loom"
)

const billingPoll = time.Minute
const billingTimeout = 10 * time.Second

type billingProbe struct {
	mu      sync.Mutex
	balance loom.CreditBalance
	success bool
	updated time.Time
}

// BillingSnapshot reads memory only; scrape callbacks never call IAM or Loom.
func (w *Worker) BillingSnapshot() (loom.CreditBalance, bool, time.Time) {
	w.billing.mu.Lock()
	defer w.billing.mu.Unlock()
	return w.billing.balance, w.billing.success, w.billing.updated
}

func (w *Worker) collectBilling(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, billingTimeout)
	defer cancel()
	balance, err := w.options.BillingRead(ctx)
	w.billing.mu.Lock()
	defer w.billing.mu.Unlock()
	w.billing.success = err == nil && ctx.Err() == nil
	if w.billing.success {
		w.billing.balance, w.billing.updated = balance, time.Now()
	}
}

// startBilling joins the bounded read loop when descriptor work exits, including failure.
func (w *Worker) startBilling(parent context.Context) func() {
	if w.options.BillingRead == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(billingPoll)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			w.collectBilling(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}
