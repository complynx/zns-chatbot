package credits

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrAccounting = errors.New("credit_accounting_unavailable")

const settleTimeout = 5 * time.Second

// Call brackets one actual provider dispatch. Each real retry starts a new
// attempt; business retries must never reuse a dispatched ID to send again.
type Call struct {
	recorder Recorder
	id       string
	usage    Usage
}

func Begin(
	ctx context.Context,
	recorder Recorder,
	operation, provider, model string,
	outputLimits ...int64,
) (*Call, error) {
	call := &Call{recorder: recorder, id: uuid.NewString(), usage: Usage{Basis: basisUnknown}}
	if recorder == nil {
		return call, nil
	}
	attempt := Attempt{
		ID:        call.id,
		Scope:     ScopeFromContext(ctx),
		Operation: operation,
		Provider:  provider,
		Model:     model,
	}
	if len(outputLimits) > 0 {
		attempt.OutputLimit = outputLimits[0]
	}
	if err := recorder.Reserve(ctx, attempt); err != nil {
		return nil, accountingError(err)
	}
	collectReceipt(ctx, call.id)
	if err := recorder.Dispatch(ctx, call.id); err != nil {
		return nil, accountingError(err)
	}
	return call, nil
}

func accountingError(err error) error {
	if errors.Is(err, ErrLimit) || errors.Is(err, ErrUnpriced) {
		return err
	}
	return ErrAccounting
}

// Capture accepts numeric metadata only. A malformed receipt remains unknown.
func (c *Call) Capture(usage Usage) {
	if usage.RequestID == "" {
		usage.RequestID = c.usage.RequestID
	}
	if usage.Validate() == nil {
		c.usage = usage
	}
}

func (c *Call) CaptureRequestID(value string) {
	if value == "" || len(value) > 256 {
		return
	}
	for _, char := range value {
		if char < 33 || char > 126 {
			return
		}
	}
	c.usage.RequestID = value
}

// Finish persists even when the request context was cancelled. A failed write
// leaves the durable dispatched attempt unresolved, never implicitly free.
func (c *Call) Finish(ctx context.Context) error {
	if c == nil || c.recorder == nil {
		return nil
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	if c.recorder.Settle(settleCtx, c.id, Settlement{Usage: c.usage, CostBasis: basisUnknown}) != nil {
		return ErrAccounting
	}
	return nil
}

// NotSent releases an attempt only when the caller knows no provider send/start
// occurred. A failed transport is uncertain and must use Finish instead.
func (c *Call) NotSent(ctx context.Context) error {
	if c == nil || c.recorder == nil {
		return nil
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	if c.recorder.NotSent(settleCtx, c.id) != nil {
		return ErrAccounting
	}
	return nil
}
