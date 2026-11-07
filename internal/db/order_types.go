package db

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/orders"
)

type OrderSource string

const (
	OrderSourceAgent    OrderSource = "agent"
	OrderSourceMiniApp  OrderSource = "miniapp"
	OrderSourceTelegram OrderSource = "telegram"
)

type OrderOperationKind string

const (
	OrderPrepare OrderOperationKind = "prepare"
	OrderSubmit  OrderOperationKind = "submit"
	OrderCancel  OrderOperationKind = "cancel"
)

type OrderSubmission struct {
	EventID                uuid.UUID `json:"event_id"`
	ExpectedEventVersion   int64     `json:"expected_event_version"   minimum:"1"`
	ExpectedCatalogVersion int64     `json:"expected_catalog_version" minimum:"1"`
	ExpectedDraftVersion   int64     `json:"expected_draft_version"   minimum:"1"`
}

// OrderService retains the selected offer's sold name and absolute service time.
type OrderService struct {
	OfferID  string       `json:"offer_id"`
	Name     orders.Label `json:"name"`
	StartsAt time.Time    `json:"starts_at"`
	EndsAt   time.Time    `json:"ends_at"`
}

// OrderSnapshot retains exactly what was quoted, including the original cutoff.
type OrderSnapshot struct {
	Meal          *MealTerms               `json:"meal,omitempty"`
	Capacity      *orders.CapacitySnapshot `json:"capacity,omitempty"`
	Services      []OrderService           `json:"services,omitempty"`
	EventName     string                   `json:"event_name"`
	Selections    []orders.Selection       `json:"selections"`
	Quote         OrdersQuoteView          `json:"quote"`
	SalesClosesAt time.Time                `json:"sales_closes_at"`
}

type OrderPreparation struct {
	Description string          `json:"description,omitempty"`
	ID          uuid.UUID       `json:"proposal_id"`
	Arguments   OrderSubmission `json:"arguments"`
	Snapshot    OrderSnapshot   `json:"snapshot"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

type SubmittedOrder struct {
	OrderKind          OrderKind     `json:"order_kind,omitempty"`
	ID                 uuid.UUID     `json:"id"`
	EventID            uuid.UUID     `json:"event_id"`
	SourceDraftVersion int64         `json:"source_draft_version"`
	Version            int64         `json:"version"`
	State              string        `json:"state"`
	Snapshot           OrderSnapshot `json:"snapshot"`
	SubmittedAt        time.Time     `json:"submitted_at"`
	CancelledAt        *time.Time    `json:"cancelled_at,omitempty"`
}

// OrderSubmissionResult is immutable; read the order separately for current state.
type OrderSubmissionResult struct {
	Order     SubmittedOrder   `json:"order"`
	Selection *OrdersSelection `json:"selection,omitempty"`
	MealDraft *MealDraft       `json:"meal_draft,omitempty"`
}

type OrderCancellation struct {
	EventID         uuid.UUID `json:"event_id"`
	OrderID         uuid.UUID `json:"order_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
}

type OrderOperation struct {
	OperationID uuid.UUID          `json:"operation_id"`
	EventID     uuid.UUID          `json:"event_id"`
	Kind        OrderOperationKind `json:"kind"`
	Result      json.RawMessage    `json:"result"`
}

type OrdersPage struct {
	Orders []SubmittedOrder `json:"orders"`
	Next   *uuid.UUID       `json:"next,omitempty"`
}

func orderJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err == nil && len(raw) > maxOrdersSelectionBytes {
		return nil, orders.ErrSelection
	}
	return raw, err
}

func orderHash(value any) ([]byte, error) {
	raw, err := orderJSON(value)
	sum := sha256.Sum256(raw)
	return sum[:], err
}

func (a OrderSubmission) valid() bool {
	return a.EventID != uuid.Nil && a.ExpectedEventVersion > 0 && a.ExpectedCatalogVersion > 0 &&
		a.ExpectedDraftVersion > 0
}

func (source OrderSource) valid() bool {
	return source == OrderSourceAgent || source == OrderSourceMiniApp || source == OrderSourceTelegram
}

func (kind OrderOperationKind) valid() bool {
	return kind == OrderPrepare || kind == OrderSubmit || kind == OrderCancel
}
