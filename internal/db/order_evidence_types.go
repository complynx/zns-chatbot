package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const (
	OrderEvidencePending       = "pending"
	OrderEvidenceExhausted     = "exhausted"
	OrderEvidenceNeedsDecision = "needs_decision"
	OrderEvidenceConfirmed     = "confirmed"
	OrderEvidenceRejected      = "rejected"
)

type OrderEvidenceSubmission struct {
	EventID              uuid.UUID `json:"event_id"`
	OrderID              uuid.UUID `json:"order_id"`
	ExpectedOrderVersion int64     `json:"expected_order_version"`
	FileID               uuid.UUID `json:"file_id"`
	AmountMinor          int64     `json:"amount_minor"`
}

// OrderPaymentEvidence reports an admission or human decision, never fulfillment.
// Submission receipts retain their original state; reads return current state.
type OrderPaymentEvidence struct {
	OrderKind         OrderKind             `json:"order_kind,omitempty"`
	Review            *OrderPaymentDecision `json:"review,omitempty"`
	ID                uuid.UUID             `json:"id"`
	OrderID           uuid.UUID             `json:"order_id"`
	EventID           uuid.UUID             `json:"event_id"`
	UserID            uuid.UUID             `json:"user_id"`
	FileID            uuid.UUID             `json:"file_id"`
	AmountMinor       int64                 `json:"amount_minor"`
	QuotedAmountMinor int64                 `json:"quoted_amount_minor"`
	Currency          string                `json:"currency"`
	CurrencyScale     int                   `json:"currency_scale"`
	SentAt            time.Time             `json:"sent_at"`
	SubmittedAt       time.Time             `json:"submitted_at"`
	DueAt             time.Time             `json:"due_at"`
	AdmissionState    string                `json:"admission_state"`
	Reason            string                `json:"reason"`
	State             string                `json:"state"`
	Version           int64                 `json:"version"`
}

func orderEvidenceRecord(row sqlc.ShuttleOrderEvidence) OrderPaymentEvidence {
	result := OrderPaymentEvidence{
		ID:                row.ID,
		OrderID:           row.OrderID,
		EventID:           row.EventID,
		UserID:            row.UserID,
		FileID:            row.FileID,
		AmountMinor:       row.ReportedAmountMinor,
		QuotedAmountMinor: row.QuotedAmountMinor,
		Currency:          row.Currency,
		CurrencyScale:     int(row.CurrencyScale),
		SentAt:            row.SentAt.Time.UTC(),
		SubmittedAt:       row.SubmittedAt.Time.UTC(),
		DueAt:             row.DueAt.Time.UTC(),
		AdmissionState:    row.AdmissionState,
		Reason:            row.Reason,
		State:             row.State,
		Version:           row.Version,
	}
	if row.OrderKind == string(OrderKindMeal) {
		result.OrderKind = OrderKindMeal
	}
	return result
}

type OrderEvidenceReview struct {
	EventID         uuid.UUID `json:"event_id"`
	EvidenceID      uuid.UUID `json:"evidence_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Decision        string    `json:"decision"`
	Reason          string    `json:"reason"`
}

// OrderEvidenceReviewPreparation binds an exact decision to the evidence revision.
type OrderEvidenceReviewPreparation struct {
	Description string               `json:"description"`
	ID          uuid.UUID            `json:"proposal_id"`
	Arguments   OrderEvidenceReview  `json:"arguments"`
	Evidence    OrderPaymentEvidence `json:"evidence"`
	ExpiresAt   time.Time            `json:"expires_at"`
}

type OrderPaymentDecision struct {
	ActorID       uuid.UUID `json:"actor_id"`
	AuthorityRole string    `json:"authority_role"`
	Decision      string    `json:"decision"`
	AmountMinor   int64     `json:"amount_minor"`
	Reason        string    `json:"reason"`
	ReviewedAt    time.Time `json:"reviewed_at"`
}

func (e OrderPaymentEvidence) businessKind() OrderKind {
	if e.OrderKind == "" {
		return OrderKindService
	}
	return e.OrderKind
}
