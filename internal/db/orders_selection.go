package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const maxOrdersSelectionBytes = 60 << 10

type OrdersSelectionChange struct {
	OperationID            uuid.UUID          `json:"operation_id"`
	EventID                uuid.UUID          `json:"event_id"`
	ExpectedEventVersion   int64              `json:"expected_event_version"   minimum:"1"`
	ExpectedCatalogVersion int64              `json:"expected_catalog_version" minimum:"1"`
	ExpectedDraftVersion   int64              `json:"expected_draft_version"   minimum:"0"`
	Selections             []orders.Selection `json:"selections"                           maxItems:"100"`
}

// OrdersSelection is a saved draft, not an order or a price guarantee. Quote is
// historical and absent after an explicit clear; an unsaved draft has version0.
type OrdersSelection struct {
	EventID        uuid.UUID          `json:"event_id"`
	EventVersion   int64              `json:"event_version"`
	CatalogVersion int64              `json:"catalog_version"`
	Version        int64              `json:"version"`
	Selections     []orders.Selection `json:"selections"`
	Quote          *OrdersQuoteView   `json:"quote,omitempty"`
	SavedAt        *time.Time         `json:"saved_at,omitempty"`
}

func normalizeOrdersSelection(a OrdersSelectionChange) (OrdersSelectionChange, []byte, error) {
	if a.OperationID == uuid.Nil || a.EventID == uuid.Nil || a.ExpectedEventVersion < 1 ||
		a.ExpectedCatalogVersion < 1 || a.ExpectedDraftVersion < 0 || a.Selections == nil || len(a.Selections) > orders.MaxSelections {
		return a, nil, orders.ErrSelection
	}
	a.Selections = append([]orders.Selection{}, a.Selections...)
	slices.SortFunc(a.Selections, func(x, y orders.Selection) int {
		if c := strings.Compare(x.OfferID, y.OfferID); c != 0 {
			return c
		}
		return strings.Compare(x.ItemID, y.ItemID)
	})
	var total int64
	for i, s := range a.Selections {
		if s.Quantity < 1 || s.Quantity > orders.MaxQuantity || s.OfferID == "" || s.ItemID == "" ||
			len(s.OfferID) > 32 || len(s.ItemID) > 32 {
			return a, nil, orders.ErrSelection
		}
		if i > 0 && a.Selections[i-1].OfferID == s.OfferID && a.Selections[i-1].ItemID == s.ItemID {
			return a, nil, orders.ErrSelection
		}
		total += s.Quantity
	}
	if total > orders.MaxTotalQuantity {
		return a, nil, orders.ErrSelection
	}
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return a, hash[:], err
}

// ordersSelectionTx follows event -> owner locking. The operation lock also
// serializes reuse of one owner's operation ID across different events.
func (s *Store) ordersSelectionTx(ctx context.Context, scope ProfileScope, event, operation uuid.UUID) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	if operation != uuid.Nil {
		err = q.LockEventOperation(ctx, fmt.Sprintf("orders-selection:%s:%s", scope.UserID, operation))
	}
	if err == nil {
		_, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
	if err == nil {
		err = authorizeOrdersSelection(ctx, tx, scope, event)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}

func authorizeOrdersSelection(ctx context.Context, tx pgx.Tx, scope ProfileScope, event uuid.UUID) error {
	if err := lockProfileOwner(ctx, tx, scope, false); err != nil {
		return err
	}
	allowed, err := sqlc.New(tx).UserModuleAvailable(ctx, sqlc.UserModuleAvailableParams{
		ID:             event,
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		ID_2:           scope.UserID,
		Column5:        orderModuleServices,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return pgx.ErrNoRows
	}
	return nil
}

// OwnOrdersSelection reads saved quantities and their original quote even if
// the current catalog no longer sells them. Current entitlement is mandatory.
func (s *Store) OwnOrdersSelection(ctx context.Context, scope ProfileScope, event uuid.UUID) (OrdersSelection, error) {
	tx, err := s.ordersSelectionTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return OrdersSelection{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOrdersSelection(ctx, s.q.WithTx(tx), scope.UserID, event)
}

func readOrdersSelection(ctx context.Context, q *sqlc.Queries, owner, event uuid.UUID) (OrdersSelection, error) {
	raw, err := q.ReadOrdersSelection(ctx, sqlc.ReadOrdersSelectionParams{UserID: owner, EventID: event})
	if errors.Is(err, pgx.ErrNoRows) {
		return OrdersSelection{EventID: event, Selections: []orders.Selection{}}, nil
	}
	var result OrdersSelection
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}

// OwnOrdersSelectionOperation recovers the immutable original receipt. The event
// is explicit, so another event's receipt cannot borrow its access permission.
func (s *Store) OwnOrdersSelectionOperation(
	ctx context.Context,
	scope ProfileScope,
	event, operation uuid.UUID,
) (OrdersSelection, error) {
	tx, err := s.ordersSelectionTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return OrdersSelection{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		ReadOrdersSelectionOperation(ctx, sqlc.ReadOrdersSelectionOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return OrdersSelection{}, err
	}
	if row.EventID != event {
		return OrdersSelection{}, pgx.ErrNoRows
	}
	var result OrdersSelection
	err = json.Unmarshal(row.Result, &result)
	return result, err
}

// SaveOwnOrdersSelection commits the exact quote, revision and receipt together.
// Unknown commit outcomes must retry the same operation and normalized body.
func (s *Store) SaveOwnOrdersSelection(
	ctx context.Context,
	scope ProfileScope,
	change OrdersSelectionChange,
) (OrdersSelection, error) {
	a, hash, err := normalizeOrdersSelection(change)
	if err != nil {
		return OrdersSelection{}, err
	}
	tx, err := s.ordersSelectionTx(ctx, scope, a.EventID, a.OperationID)
	if err != nil {
		return OrdersSelection{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.saveOrdersSelection(ctx, tx, scope, a, hash)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// saveOrdersSelection requires the event and current owner locks. It uses no
// second connection and never commits; session wrappers can journal atomically.
func (s *Store) saveOrdersSelection(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	a OrdersSelectionChange,
	hash []byte,
) (OrdersSelection, bool, error) {
	q := s.q.WithTx(tx)
	prior, err := q.ReadOrdersSelectionOperation(
		ctx,
		sqlc.ReadOrdersSelectionOperationParams{UserID: scope.UserID, OperationID: a.OperationID},
	)
	if err == nil {
		if prior.EventID != a.EventID || !bytes.Equal(prior.RequestHash, hash) {
			return OrdersSelection{}, true, ErrConflict
		}
		var result OrdersSelection
		err = json.Unmarshal(prior.Result, &result)
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrdersSelection{}, false, err
	}
	snapshot, err := s.ownOrdersCatalogSnapshot(ctx, tx, scope, a.EventID)
	if err != nil {
		return OrdersSelection{}, false, err
	}
	if snapshot.EventVersion != a.ExpectedEventVersion || snapshot.Version != a.ExpectedCatalogVersion {
		return OrdersSelection{}, false, ErrConflict
	}
	current, err := readOrdersSelection(ctx, q, scope.UserID, a.EventID)
	if err != nil {
		return OrdersSelection{}, false, err
	}
	if current.Version != a.ExpectedDraftVersion || current.Version == int64(^uint64(0)>>1) {
		return OrdersSelection{}, false, ErrConflict
	}
	result := OrdersSelection{EventID: a.EventID, EventVersion: snapshot.EventVersion, CatalogVersion: snapshot.Version,
		Version: current.Version + 1, Selections: a.Selections, SavedAt: &snapshot.ObservedAt}
	if len(a.Selections) != 0 {
		document := orders.CatalogDocument{
			Currency:         snapshot.Currency,
			CurrencyScale:    snapshot.CurrencyScale,
			Items:            snapshot.Items,
			CompletePricing:  snapshot.CompletePricing,
			CompleteCapacity: snapshot.CompleteCapacity,
		}
		for _, offer := range snapshot.Offers {
			document.Offers = append(document.Offers, offer.Offer)
		}
		quote, quoteErr := orders.Calculate(document, a.Selections, snapshot.ObservedAt)
		if quoteErr != nil {
			return OrdersSelection{}, false, quoteErr
		}
		result.Quote = &OrdersQuoteView{
			Quote:        quote,
			EventID:      a.EventID,
			EventVersion: snapshot.EventVersion,
			Version:      snapshot.Version,
			Timezone:     snapshot.Timezone,
			ObservedAt:   snapshot.ObservedAt,
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return OrdersSelection{}, false, err
	}
	if len(raw) > maxOrdersSelectionBytes {
		return OrdersSelection{}, false, orders.ErrSelection
	}
	if err = q.SaveOrdersSelection(
		ctx,
		sqlc.SaveOrdersSelectionParams{UserID: scope.UserID, EventID: a.EventID, Version: result.Version, Result: raw},
	); err != nil {
		return OrdersSelection{}, false, err
	}
	err = q.InsertOrdersSelectionOperation(
		ctx,
		sqlc.InsertOrdersSelectionOperationParams{
			UserID:      scope.UserID,
			OperationID: a.OperationID,
			EventID:     a.EventID,
			RequestHash: hash,
			Result:      raw,
		},
	)
	return result, false, err
}

// OwnOrdersSelectionOperationByID supports Loom status probes, which carry only
// an operation ID. Resolve its event privately, release the owner-only lock, then
// recheck entitlement in event -> owner order before disclosing the receipt.
func (s *Store) OwnOrdersSelectionOperationByID(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (OrdersSelection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrdersSelection{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return OrdersSelection{}, err
	}
	row, err := s.q.WithTx(tx).
		ReadOrdersSelectionOperation(ctx, sqlc.ReadOrdersSelectionOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return OrdersSelection{}, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return OrdersSelection{}, err
	}
	return s.OwnOrdersSelectionOperation(ctx, scope, row.EventID, operation)
}
