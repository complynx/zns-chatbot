// Package payments separates submitted evidence from human-confirmed money.
package payments

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

const (
	MaxAmount       int64 = 1<<53 - 1
	Initial               = "initial"
	TopUp                 = "top_up"
	Pending               = "pending"
	Confirmed             = "confirmed"
	Rejected              = "rejected"
	MaxReasonLength       = 512
)

var (
	ErrInvalid     = errors.New("invalid payment request")
	ErrDenied      = errors.New("event payment authority required")
	ErrUnavailable = errors.New("payment module unavailable")
)

type Submission struct {
	EventID       uuid.UUID `json:"event_id"`
	PassID        uuid.UUID `json:"pass_id"`
	FileID        uuid.UUID `json:"file_id"`
	AmountMinor   int64     `json:"amount_minor" minimum:"1" maximum:"9007199254740991"`
	Currency      string    `json:"currency"`
	CurrencyScale int32     `json:"currency_scale" minimum:"0" maximum:"4"`
	Purpose       string    `json:"purpose" enum:"initial,top_up"`
}

func (s Submission) Valid() bool {
	return s.EventID != uuid.Nil && s.PassID != uuid.Nil && s.FileID != uuid.Nil && s.AmountMinor > 0 &&
		s.AmountMinor <= MaxAmount &&
		s.CurrencyScale >= 0 &&
		s.CurrencyScale <= 4 &&
		(s.Purpose == Initial || s.Purpose == TopUp)
}

type Evidence struct {
	Review        *Review   `json:"review,omitempty"`
	ID            uuid.UUID `json:"id"`
	EventID       uuid.UUID `json:"event_id"`
	PassID        uuid.UUID `json:"pass_id"`
	UserID        uuid.UUID `json:"user_id"`
	FileID        uuid.UUID `json:"file_id"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	CurrencyScale int32     `json:"currency_scale"`
	Purpose       string    `json:"purpose"`
	SentAt        time.Time `json:"sent_at"`
	SubmittedAt   time.Time `json:"submitted_at"`
	DueAt         time.Time `json:"due_at"`
	Timely        bool      `json:"timely"`
	State         string    `json:"state"`
}
type ReviewChange struct {
	EventID         uuid.UUID `json:"event_id"`
	EvidenceID      uuid.UUID `json:"evidence_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"0"`
	Decision        string    `json:"decision" enum:"confirmed,rejected"`
	AmountMinor     int64     `json:"amount_minor" minimum:"0" maximum:"9007199254740991"`
	Reason          string    `json:"reason" maxLength:"512"`
}

func (c ReviewChange) Normalize() (ReviewChange, error) {
	c.Reason = strings.TrimSpace(c.Reason)
	if c.EventID == uuid.Nil || c.EvidenceID == uuid.Nil || c.ExpectedVersion < 0 ||
		len([]rune(c.Reason)) > MaxReasonLength {
		return ReviewChange{}, ErrInvalid
	}
	switch c.Decision {
	case Confirmed:
		if c.AmountMinor <= 0 || c.AmountMinor > MaxAmount {
			return ReviewChange{}, ErrInvalid
		}
	case Rejected:
		if c.AmountMinor != 0 || c.Reason == "" {
			return ReviewChange{}, ErrInvalid
		}
	default:
		return ReviewChange{}, ErrInvalid
	}
	return c, nil
}

type Review struct {
	ID            uuid.UUID `json:"id"`
	EvidenceID    uuid.UUID `json:"evidence_id"`
	ActorID       uuid.UUID `json:"actor_id"`
	AuthorityRole string    `json:"authority_role"`
	Decision      string    `json:"decision"`
	AmountMinor   int64     `json:"amount_minor"`
	Reason        string    `json:"reason"`
	ReviewedAt    time.Time `json:"reviewed_at"`
}
type Balance struct {
	PassID               uuid.UUID `json:"pass_id"`
	Version              int64     `json:"version"`
	PriceMinor           int64     `json:"price_minor"`
	Currency             string    `json:"currency"`
	CurrencyScale        int32     `json:"currency_scale"`
	ConfirmedMinor       int64     `json:"confirmed_minor"`
	RemainingMinor       int64     `json:"remaining_minor"`
	DepositRequiredMinor int64     `json:"deposit_required_minor"`
	DepositSatisfied     bool      `json:"deposit_satisfied"`
	FullyPaid            bool      `json:"fully_paid"`
}

// Summarize uses only confirmed ledger amounts. A zero-minimum policy still needs
// a positive confirmed payment for a priced pass; free passes need no transfer.
func Summarize(pass registrations.Pass, version, total int64) (Balance, error) {
	if pass.PriceMinor < 0 || pass.PriceMinor > MaxAmount || version < 0 || total < 0 || total > pass.PriceMinor {
		return Balance{}, ErrInvalid
	}
	required := pass.PriceMinor
	if pass.Deposit != nil {
		required = pass.Deposit.RequiredMinor
		if required < 0 || required > pass.PriceMinor {
			return Balance{}, ErrInvalid
		}
	}
	full := total == pass.PriceMinor
	return Balance{
		PassID:               pass.ID,
		Version:              version,
		PriceMinor:           pass.PriceMinor,
		Currency:             pass.Currency,
		CurrencyScale:        pass.CurrencyScale,
		ConfirmedMinor:       total,
		RemainingMinor:       pass.PriceMinor - total,
		DepositRequiredMinor: required,
		DepositSatisfied:     full || total > 0 && total >= required,
		FullyPaid:            full,
	}, nil
}
func Deadline(pass registrations.Pass, purpose string) (time.Time, error) {
	switch purpose {
	case Initial:
		return pass.PaymentDueAt, nil
	case TopUp:
		if pass.Deposit != nil {
			return pass.Deposit.FinalDueAt, nil
		}
	}
	return time.Time{}, ErrInvalid
}

type ReviewResult struct {
	Evidence Evidence `json:"evidence"`
	Review   Review   `json:"review"`
	Balance  Balance  `json:"balance"`
}
