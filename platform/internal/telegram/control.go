package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const controlCompletionTimeout = 5 * time.Second

// ControlPolicy owns short bot-pacing transactions, never transport or retries.
type ControlPolicy interface {
	Admit(context.Context) (delivery.Admission, error)
	Observe(context.Context, delivery.Outcome) (delivery.Outcome, time.Time, error)
}

// ControlError reports a control call deferred or paused by the durable bot scope.
// It contains no Telegram payload or credentials.
type ControlError struct {
	Reason    string
	NotBefore time.Time
	retryable bool
	cause     error
}

func (e *ControlError) Error() string { return "telegram control unavailable: " + e.Reason }
func (e *ControlError) Unwrap() error { return e.cause }

func controlMethod(method string) bool {
	switch method {
	case "getMe", "getUpdates", "getFile", "getChat", "setMyCommands", "setChatMenuButton", "answerCallbackQuery":
		return true
	default:
		return false
	}
}

func (c Client) admitControl(ctx context.Context, method string) error {
	if c.Control == nil || !controlMethod(method) {
		return nil
	}
	admission, err := c.Control.Admit(ctx)
	if err != nil {
		return err
	}
	if admission.Ready {
		return nil
	}
	return &ControlError{
		Reason:    admission.Reason,
		NotBefore: admission.NotBefore,
		retryable: admission.Reason == "delivery_cooldown",
	}
}

func (c Client) observeControl(ctx context.Context, method string, wireErr error) error {
	if wireErr == nil || c.Control == nil || !controlMethod(method) {
		return wireErr
	}
	outcome := DeliveryOutcome(0, wireErr)
	if outcome.Kind != delivery.Deferred && outcome.Kind != delivery.Parked && outcome.Kind != delivery.Paused {
		return wireErr
	}
	completion, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlCompletionTimeout)
	defer cancel()
	result, deadline, err := c.Control.Observe(completion, outcome)
	if err != nil {
		return errors.Join(wireErr, err)
	}
	return &ControlError{
		Reason:    result.Reason,
		NotBefore: deadline,
		retryable: result.Kind == delivery.Deferred,
		cause:     wireErr,
	}
}

// RetryControl is for startup reads or idempotent configuration replacement.
// Only durable finite admission deferrals and definite429 responses are retried.
// The callback and waiting run without a database transaction. Unknown outcomes,
// paused scopes and storage errors return immediately.
func RetryControl(ctx context.Context, operation func(context.Context) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation(ctx)
		deferred, ok := errors.AsType[*ControlError](err)
		if !ok || !deferred.retryable || deferred.NotBefore.IsZero() {
			return err
		}
		timer := time.NewTimer(max(time.Until(deferred.NotBefore), 0))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func decodeControlRateLimit(body io.Reader) error {
	var response struct {
		OK         bool               `json:"ok"`
		Code       int                `json:"error_code"`
		Parameters ResponseParameters `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(body, maxResponseBytes)).
		Decode(&response); err != nil || response.OK ||
		response.Code != http.StatusTooManyRequests {
		return invalidRateLimitResponse()
	}
	return &APIError{
		Code:        http.StatusTooManyRequests,
		Description: "file download rate limited",
		Parameters:  response.Parameters,
	}
}
