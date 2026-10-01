package registrationingress

import (
	"context"
	"errors"
	"time"
)

// Clock supplies trusted registration-domain time. Runtime composition owns the
// binding; updates, commands and model input never carry it.
type Clock interface {
	Now(context.Context) (time.Time, error)
}

type clockKey struct{}

func WithClock(ctx context.Context, clock Clock) context.Context {
	if clock == nil {
		return ctx
	}
	return context.WithValue(ctx, clockKey{}, clock)
}

// Observe leaves SQL defaults authoritative when no clock is configured.
func Observe(ctx context.Context, clock Clock) (time.Time, bool, error) {
	if clock == nil {
		return time.Time{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return time.Time{}, true, err
	}
	now, err := clock.Now(ctx)
	if canceled := ctx.Err(); canceled != nil {
		return time.Time{}, true, errors.Join(err, canceled)
	}
	if err != nil {
		return time.Time{}, true, err
	}
	if now.IsZero() || now.Nanosecond()%1000 != 0 {
		return time.Time{}, true, errors.New("registration clock requires a nonzero microsecond instant")
	}
	now = now.UTC()
	return now, true, nil
}

func observeContext(ctx context.Context) (time.Time, bool, error) {
	clock, _ := ctx.Value(clockKey{}).(Clock)
	return Observe(ctx, clock)
}
