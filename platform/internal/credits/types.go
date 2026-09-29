// Package credits records paid attempts without changing user allowances.
package credits

import (
	"context"
	"errors"
)

var ErrConflict = errors.New("credit_attempt_conflict")
var ErrInvalid = errors.New("credit_usage_invalid")

// Usage carries provider counters, never prompts, results or credentials.
// Nil counters are unknown, including on failed or truncated responses.
type Usage struct {
	RequestID    string  `json:"request_id,omitempty"`
	ResponseID   string  `json:"response_id,omitempty"`
	Model        string  `json:"model,omitempty"`
	ServiceTier  string  `json:"service_tier,omitempty"`
	Input        *int64  `json:"input_tokens,omitempty"`
	Cached       *int64  `json:"cached_input_tokens,omitempty"`
	CacheWrite   *int64  `json:"cache_write_tokens,omitempty"`
	Output       *int64  `json:"output_tokens,omitempty"`
	Reasoning    *int64  `json:"reasoning_tokens,omitempty"`
	AudioSeconds *string `json:"audio_seconds,omitempty"`
	AudioInput   *int64  `json:"audio_input_tokens,omitempty"`
	TextInput    *int64  `json:"text_input_tokens,omitempty"`
	Basis        string  `json:"basis"`
}

type Scope struct {
	LegacyBudget bool   `json:"legacy_budget,omitempty"`
	Actor        string `json:"actor"`
	Payer        string `json:"payer"`
	Key          string `json:"key"`
}
type Attempt struct {
	ID              string `json:"id"`
	Scope           Scope  `json:"scope"`
	Operation       string `json:"operation"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReservedNanoUSD *int64 `json:"reserved_nano_usd"`
	OutputLimit     int64  `json:"output_limit"`
}
type Settlement struct {
	Usage             Usage  `json:"usage"`
	CostNanoUSD       *int64 `json:"cost_nano_usd,omitempty"`
	CostBasis         string `json:"cost_basis"`
	PriceVersion      string `json:"price_version,omitempty"`
	ConversionVersion string `json:"conversion_version,omitempty"`
	OriginalAmount    string `json:"original_amount,omitempty"`
	OriginalCurrency  string `json:"original_currency,omitempty"`
}

type Recorder interface {
	RequestTier() string
	Reserve(context.Context, Attempt) error
	Dispatch(context.Context, string) error
	NotSent(context.Context, string) error
	Settle(context.Context, string, Settlement) error
}

type scopeKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}
func ScopeFromContext(ctx context.Context) Scope {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	if !ok {
		return Scope{Actor: "system", Payer: "system", Key: "unattributed"}
	}
	return scope
}

func (u Usage) Validate() error {
	if len(u.RequestID) > 256 || len(u.ResponseID) > 256 || len(u.Model) > 200 || len(u.ServiceTier) > 64 {
		return ErrInvalid
	}
	if u.Basis != basisReported && u.Basis != basisEstimated && u.Basis != basisUnknown && u.Basis != basisFree {
		return ErrInvalid
	}
	for _, count := range []*int64{u.Input, u.Cached, u.CacheWrite, u.Output, u.Reasoning, u.AudioInput, u.TextInput} {
		if count != nil && *count < 0 {
			return ErrInvalid
		}
	}
	if !validSubsets(u.Input, u.AudioInput, u.TextInput) || !validSubsets(u.Input, u.Cached, u.CacheWrite) ||
		!validSubsets(u.Output, u.Reasoning) {
		return ErrInvalid
	}
	return validateAudioSeconds(u.AudioSeconds)
}
func validSubsets(total *int64, parts ...*int64) bool {
	if total == nil {
		return true
	}
	remaining := *total
	for _, part := range parts {
		if part != nil {
			if *part > remaining {
				return false
			}
			remaining -= *part
		}
	}
	return true
}
func validateAudioSeconds(value *string) error {
	if value == nil {
		return nil
	}
	seconds, ok := boundedRatio(*value)
	if !ok || seconds.Sign() < 0 {
		return ErrInvalid
	}
	return nil
}

const (
	basisUnknown   = "unknown"
	basisReported  = "reported"
	basisEstimated = "estimated"
	basisFree      = "known_free"
)
