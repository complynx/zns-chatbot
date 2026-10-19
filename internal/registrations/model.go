// Package registrations models solo registrations and assigned-pass access.
package registrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

var (
	ErrInvalid     = errors.New("invalid registration request")
	ErrCapacity    = errors.New("event role capacity reached")
	ErrUnavailable = errors.New("registration module unavailable")
)

const (
	Leader           = "leader"
	Follower         = "follower"
	Waitlisted       = "waitlisted"
	Assigned         = "assigned"
	maxCurrencyScale = 4
	maxJSONInteger   = 1<<53 - 1
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// Rules are explicit event configuration; price units use the declared scale.
// No currency conversion, expiry cancellation or automatic allocation is implied.
type Rules struct {
	DetailsRequirements  *registrationdetails.Requirements `json:"details_requirements,omitempty"`
	Pricing              *TierPolicy                       `json:"pricing,omitempty"`
	LeaderCapacity       int32                             `json:"leader_capacity"`
	FollowerCapacity     int32                             `json:"follower_capacity"`
	PriceMinor           int64                             `json:"price_minor"                    minimum:"0" maximum:"9007199254740991"`
	Currency             string                            `json:"currency"`
	CurrencyScale        int32                             `json:"currency_scale"`
	ClosesAt             time.Time                         `json:"closes_at"`
	PaymentWindowSeconds int32                             `json:"payment_window_seconds"`
}

// UnmarshalJSON requires explicit fields, including zero-valid price and capacities.
func (r *Rules) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ErrInvalid
	}
	for _, name := range []string{"leader_capacity", "follower_capacity", "price_minor", "currency", "currency_scale", "closes_at", "payment_window_seconds"} {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	type document Rules
	var decoded document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return ErrInvalid
	}
	normalized, err := Rules(decoded).Normalize()
	if err != nil {
		return err
	}
	*r = normalized
	return nil
}

func (r Rules) Normalize() (Rules, error) {
	r.Currency = strings.TrimSpace(r.Currency)
	if r.LeaderCapacity < 0 || r.FollowerCapacity < 0 ||
		r.PriceMinor < 0 || r.PriceMinor > maxJSONInteger || !currencyCode.MatchString(r.Currency) ||
		r.CurrencyScale < 0 || r.CurrencyScale > maxCurrencyScale || r.ClosesAt.IsZero() || r.ClosesAt.Nanosecond()%int(time.Microsecond) != 0 ||
		r.PaymentWindowSeconds < 1 {
		return Rules{}, ErrInvalid
	}
	if r.DetailsRequirements != nil {
		requirements := r.DetailsRequirements.Normalize()
		r.DetailsRequirements = &requirements
	}
	r.ClosesAt = r.ClosesAt.UTC()
	if r.Pricing != nil {
		policy, err := r.Pricing.Normalize()
		if err != nil {
			return Rules{}, err
		}
		r.Pricing = &policy
	}
	return r, nil
}

func ValidRole(role string) bool { return role == Leader || role == Follower }

func (r Rules) Capacity(role string) (int32, error) {
	switch role {
	case Leader:
		return r.LeaderCapacity, nil
	case Follower:
		return r.FollowerCapacity, nil
	default:
		return 0, ErrInvalid
	}
}

// PaymentDueAt limits the configured payment window to sales closing.
// Passing this deadline changes an expectation, not the assigned pass's state.
func (r Rules) PaymentDueAt(assignedAt time.Time) time.Time {
	due := assignedAt.Add(time.Duration(r.PaymentWindowSeconds) * time.Second)
	if due.After(r.ClosesAt) {
		due = r.ClosesAt
	}
	return due.UTC()
}

type Registration struct {
	ID          uuid.UUID `json:"id"`
	EventID     uuid.UUID `json:"event_id"`
	UserID      uuid.UUID `json:"user_id"`
	Role        string    `json:"role"`
	State       string    `json:"state"`
	Version     int64     `json:"version"`
	RequestedAt time.Time `json:"requested_at"`
	Pass        *Pass     `json:"pass,omitempty"`
}

// Pass freezes price, currency and deadline independently of later event edits.
type Pass struct {
	Tier           *TierSnapshot    `json:"tier,omitempty"`
	Deposit        *DepositSnapshot `json:"deposit,omitempty"`
	ID             uuid.UUID        `json:"id"`
	RegistrationID uuid.UUID        `json:"registration_id"`
	PriceMinor     int64            `json:"price_minor"`
	Currency       string           `json:"currency"`
	CurrencyScale  int32            `json:"currency_scale"`
	AssignedAt     time.Time        `json:"assigned_at"`
	PaymentDueAt   time.Time        `json:"payment_due_at"`
}
