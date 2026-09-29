package telegram

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// UnmarshalJSON retains a structured 429 even when its cooldown is unusable.
// Callers can park it instead of losing the rejection classification.
func (p *ResponseParameters) UnmarshalJSON(data []byte) error {
	*p = decodeResponseParameters(data)
	return nil
}

func decodeResponseParameters(data []byte) ResponseParameters {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ResponseParameters{RetryAfterInvalid: true}
	}
	raw, found := fields["retry_after"]
	if !found {
		return ResponseParameters{}
	}
	p := ResponseParameters{RetryAfterPresent: true}
	if string(raw) == "null" {
		p.RetryAfterInvalid = true
		return p
	}
	if err := json.Unmarshal(raw, &p.RetryAfter); err != nil {
		p.RetryAfterInvalid = true
	}
	return p
}

// DeliveryOutcome classifies a completed wire call; cancellation after dispatch
// is uncertain, just like an accepted send whose response was lost.
func DeliveryOutcome(messageID int64, err error) delivery.Outcome {
	if err == nil && messageID > 0 {
		return delivery.Outcome{Kind: delivery.Succeeded, MessageID: messageID}
	}
	result := delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"}
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		return result
	}
	switch apiErr.Code {
	case http.StatusTooManyRequests:
		p := apiErr.Parameters
		if p.RetryAfterInvalid || p.RetryAfter < 0 {
			return delivery.Outcome{Kind: delivery.Parked, Reason: "telegram_invalid_cooldown"}
		}
		return delivery.Outcome{
			Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: p.RetryAfter,
			Missing: !p.RetryAfterPresent && p.RetryAfter == 0,
		}
	case http.StatusUnauthorized, http.StatusNotFound:
		return delivery.Outcome{Kind: delivery.Paused, Reason: "telegram_service_rejected"}
	case http.StatusBadRequest, http.StatusForbidden:
		return delivery.Outcome{Kind: delivery.Rejected, Reason: "telegram_recipient_rejected"}
	default:
		return result
	}
}
