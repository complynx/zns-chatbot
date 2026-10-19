package registrations

import (
	"bytes"
	"encoding/json"
	"time"
)

const BasisPoints = 10000

// ValidatePrices checks that the minimum can be met at every configured price.
func (p DepositPolicy) ValidatePrices(rules Rules) error {
	if rules.Pricing == nil {
		_, err := p.Threshold(rules.PriceMinor)
		return err
	}
	for _, tier := range rules.Pricing.Tiers {
		if _, err := p.Threshold(tier.PriceMinor); err != nil {
			return err
		}
	}
	return nil
}

// DepositPolicy requires both configured minima; amounts use the pass currency scale.
// A missing policy requires full confirmation to satisfy the payment expectation.
// This policy never cancels a pass.
type DepositPolicy struct {
	MinimumMinor int64 `json:"minimum_minor" minimum:"0" maximum:"9007199254740991"`
	MinimumBPS   int32 `json:"minimum_bps"   minimum:"0" maximum:"10000"`
}

func (p *DepositPolicy) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ErrInvalid
	}
	for _, name := range []string{"minimum_minor", "minimum_bps"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	type policy DepositPolicy
	var decoded policy
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&decoded); err != nil {
		return ErrInvalid
	}
	if _, err := DepositPolicy(decoded).Threshold(maxJSONInteger); err != nil {
		return err
	}
	*p = DepositPolicy(decoded)
	return nil
}

// Threshold rounds the share upward without multiplying the entire price.
func (p DepositPolicy) Threshold(price int64) (int64, error) {
	if price < 0 || price > maxJSONInteger || p.MinimumMinor < 0 || p.MinimumMinor > price || p.MinimumBPS < 0 ||
		p.MinimumBPS > BasisPoints {
		return 0, ErrInvalid
	}
	share := price / BasisPoints * int64(p.MinimumBPS)
	remainder := price % BasisPoints * int64(p.MinimumBPS)
	share += (remainder + BasisPoints - 1) / BasisPoints
	return max(p.MinimumMinor, share), nil
}

// DepositSnapshot freezes the agreed threshold and final deadline at assignment.
type DepositSnapshot struct {
	MinimumMinor  int64     `json:"minimum_minor"`
	MinimumBPS    int32     `json:"minimum_bps"`
	RequiredMinor int64     `json:"required_minor"`
	FinalDueAt    time.Time `json:"final_due_at"`
}

func (p DepositPolicy) Snapshot(price int64, closesAt time.Time) (DepositSnapshot, error) {
	required, err := p.Threshold(price)
	if err != nil || closesAt.IsZero() || closesAt.Nanosecond()%int(time.Microsecond) != 0 {
		return DepositSnapshot{}, ErrInvalid
	}
	return DepositSnapshot{
		MinimumMinor:  p.MinimumMinor,
		MinimumBPS:    p.MinimumBPS,
		RequiredMinor: required,
		FinalDueAt:    closesAt.UTC(),
	}, nil
}
