package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

type OrdersCatalog struct {
	EventID  uuid.UUID              `json:"event_id"`
	Version  int64                  `json:"version"`
	Document orders.CatalogDocument `json:"document"`
}

type OrdersCatalogChange struct {
	EventID              uuid.UUID              `json:"event_id"`
	ExpectedEventVersion int64                  `json:"expected_event_version" minimum:"1"`
	ExpectedVersion      int64                  `json:"expected_version"       minimum:"0"`
	Document             orders.CatalogDocument `json:"document"`
}

type OrdersCatalogPreparation struct {
	ID        uuid.UUID           `json:"proposal_id"`
	Arguments OrdersCatalogChange `json:"arguments"`
	ExpiresAt time.Time           `json:"expires_at"`
}

type OrdersOfferView struct {
	orders.Offer

	SalesOpen bool `json:"sales_open"`
}

type OrdersCatalogView struct {
	EventID          uuid.UUID                `json:"event_id"`
	EventVersion     int64                    `json:"event_version"`
	EventName        string                   `json:"event_name"`
	Timezone         string                   `json:"timezone"`
	Version          int64                    `json:"version"`
	ObservedAt       time.Time                `json:"observed_at"`
	Currency         string                   `json:"currency"`
	CurrencyScale    int                      `json:"currency_scale"`
	Items            []orders.Item            `json:"items"`
	Offers           []OrdersOfferView        `json:"offers"`
	PricesAreBase    bool                     `json:"prices_are_base"`
	CompletePricing  *orders.CompletePricing  `json:"complete_pricing,omitempty"`
	CompleteCapacity *orders.CompleteCapacity `json:"complete_capacity,omitempty"`
}

func ordersCatalogRecord(row sqlc.ShuttleOrdersCatalog) (OrdersCatalog, error) {
	result := OrdersCatalog{EventID: row.EventID, Version: row.Version}
	if err := json.Unmarshal(row.Document, &result.Document); err != nil {
		return result, err
	}
	normalized, err := orders.Normalize(result.Document)
	result.Document = normalized
	return result, err
}

// OrdersCatalogConfiguration is an administrative configuration read. The API
// requires the global administrator role; ordinary owners use OwnOrdersCatalog.
func (s *Store) OrdersCatalogConfiguration(
	ctx context.Context,
	scope ProfileScope,
	eventID uuid.UUID,
) (OrdersCatalog, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrdersCatalog{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return OrdersCatalog{}, err
	}
	q := s.q.WithTx(tx)
	if _, err = q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return OrdersCatalog{}, err
	}
	row, err := q.ReadOrdersCatalog(ctx, eventID)
	if err != nil {
		return OrdersCatalog{}, err
	}
	return ordersCatalogRecord(row)
}

// OwnOrdersCatalog requires the caller's assigned pass, including unpaid/overdue.
// Current binding, module entitlement and catalog share one transaction connection.
func (s *Store) OwnOrdersCatalog(
	ctx context.Context,
	scope ProfileScope,
	eventID uuid.UUID,
) (OrdersCatalogView, error) {
	result := OrdersCatalogView{PricesAreBase: true}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return s.ownOrdersCatalogSnapshot(ctx, tx, scope, eventID)
}

// ownOrdersCatalogSnapshot uses the caller's transaction for current ownership,
// entitlement, catalog and database time. Writers must acquire their event lock
// before calling it; this helper never opens another connection or commits.
func (s *Store) ownOrdersCatalogSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	eventID uuid.UUID,
) (OrdersCatalogView, error) {
	result := OrdersCatalogView{PricesAreBase: true}
	if err := lockProfileOwner(ctx, tx, scope, false); err != nil {
		return result, err
	}
	q := s.q.WithTx(tx)
	allowed, err := q.UserModuleAvailable(ctx, sqlc.UserModuleAvailableParams{
		ID:             eventID,
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		ID_2:           scope.UserID,
		Column5:        orderModuleServices,
	})
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, pgx.ErrNoRows
	}
	event, err := q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return result, err
	}
	row, err := q.ReadOrdersCatalog(ctx, eventID)
	if err != nil {
		return result, err
	}
	catalog, err := ordersCatalogRecord(row)
	if err != nil {
		return result, err
	}
	zone, err := time.LoadLocation(event.Timezone)
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	result.EventID, result.EventVersion, result.EventName, result.Timezone = eventID, event.Version, event.Name, event.Timezone
	result.Version, result.ObservedAt = catalog.Version, now.Time.UTC()
	result.Currency, result.CurrencyScale, result.Items = catalog.Document.Currency, catalog.Document.CurrencyScale, catalog.Document.Items
	result.CompletePricing = catalog.Document.CompletePricing
	result.CompleteCapacity = catalog.Document.CompleteCapacity
	result.Offers = make([]OrdersOfferView, 0, len(catalog.Document.Offers))
	for _, offer := range catalog.Document.Offers {
		open := offer.SalesOpen(result.ObservedAt)
		offer.StartsAt, offer.EndsAt = offer.StartsAt.In(zone), offer.EndsAt.In(zone)
		offer.SalesOpensAt, offer.SalesClosesAt = offer.SalesOpensAt.In(zone), offer.SalesClosesAt.In(zone)
		result.Offers = append(result.Offers, OrdersOfferView{Offer: offer, SalesOpen: open})
	}
	return result, nil
}

func normalizeOrdersChange(a OrdersCatalogChange) (OrdersCatalogChange, []byte, error) {
	if a.EventID == uuid.Nil || a.ExpectedEventVersion < 1 || a.ExpectedVersion < 0 {
		return a, nil, orders.ErrInvalid
	}
	document, err := orders.Normalize(a.Document)
	if err != nil {
		return a, nil, err
	}
	a.Document = document
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return a, hash[:], err
}

// Lock order is event then current actor, matching other event mutations.
func (s *Store) ordersCatalogTx(
	ctx context.Context,
	scope ProfileScope,
	operation, eventID uuid.UUID,
) (pgx.Tx, *sqlc.Queries, int64, error) {
	if operation == uuid.Nil {
		return nil, nil, 0, orders.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(ctx, fmt.Sprintf("orders-catalog:%s:%s", scope.UserID, operation))
	var event sqlc.ShuttleEvent
	if err == nil {
		event, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
	if err == nil {
		err = lockProfileOwner(ctx, tx, scope, false)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, 0, err
	}
	return tx, q, event.Version, nil
}

func ordersCatalogVersion(
	ctx context.Context,
	q *sqlc.Queries,
	eventID uuid.UUID,
	expected int64,
	capacity *orders.CompleteCapacity,
) error {
	row, err := q.ReadOrdersCatalog(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) && expected == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Version != expected {
		return ErrConflict
	}
	prior, err := ordersCatalogRecord(row)
	if err != nil {
		return err
	}
	if !orders.RetainsCapacityResources(prior.Document.CompleteCapacity, capacity) {
		return ErrConflict
	}
	return checkOrderResourceCeilings(ctx, q, eventID, capacity)
}

// PrepareOrdersCatalog records a five-minute exact proposal without changing the catalog.
func (s *Store) PrepareOrdersCatalog(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	a OrdersCatalogChange,
) (OrdersCatalogPreparation, error) {
	var result OrdersCatalogPreparation
	a, hash, err := normalizeOrdersChange(a)
	if err != nil {
		return result, err
	}
	tx, q, eventVersion, err := s.ordersCatalogTx(ctx, scope, operation, a.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadOrdersCatalogPreparation(
		ctx,
		sqlc.ReadOrdersCatalogPreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return result, ErrConflict
		}
		return OrdersCatalogPreparation{ID: prior.ID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if eventVersion != a.ExpectedEventVersion {
		return result, ErrConflict
	}
	if err = ordersCatalogVersion(ctx, q, a.EventID, a.ExpectedVersion, a.Document.CompleteCapacity); err != nil {
		return result, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return result, err
	}
	row, err := q.InsertOrdersCatalogPreparation(
		ctx,
		sqlc.InsertOrdersCatalogPreparationParams{
			ID:          uuid.New(),
			UserID:      scope.UserID,
			OperationID: operation,
			RequestHash: hash,
			Arguments:   raw,
		},
	)
	if err != nil {
		return result, err
	}
	result = OrdersCatalogPreparation{ID: row.ID, Arguments: a, ExpiresAt: row.ExpiresAt.Time.UTC()}
	return result, tx.Commit(ctx)
}

// ChangeOrdersCatalog atomically stores the normalized catalog and original receipt.
// A successful same-operation replay precedes expiry/version checks.
func (s *Store) ChangeOrdersCatalog(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	a OrdersCatalogChange,
	checkIdentity func(context.Context) error,
) (OrdersCatalog, error) {
	var result OrdersCatalog
	a, hash, err := normalizeOrdersChange(a)
	if err != nil || proposal == uuid.Nil || checkIdentity == nil {
		return result, orders.ErrInvalid
	}
	tx, q, eventVersion, err := s.ordersCatalogTx(ctx, scope, operation, a.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadOrdersCatalogOperation(
		ctx,
		sqlc.ReadOrdersCatalogOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return result, ErrConflict
		}
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	prepared, err := q.ReadOrdersCatalogPreparation(
		ctx,
		sqlc.ReadOrdersCatalogPreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	if prepared.ID != proposal || !bytes.Equal(prepared.RequestHash, hash) ||
		!prepared.ExpiresAt.Time.After(now.Time) ||
		eventVersion != a.ExpectedEventVersion {
		return result, ErrConflict
	}
	if err = ordersCatalogVersion(ctx, q, a.EventID, a.ExpectedVersion, a.Document.CompleteCapacity); err != nil {
		return result, err
	}
	if err = checkIdentity(ctx); err != nil {
		return result, err
	}
	result, err = writeOrdersCatalog(ctx, q, a)
	if err != nil {
		return result, err
	}
	receipt, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if err = q.InsertOrdersCatalogOperation(ctx, sqlc.InsertOrdersCatalogOperationParams{
		UserID: scope.UserID, OperationID: operation, RequestHash: hash, Result: receipt,
	}); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func writeOrdersCatalog(ctx context.Context, q *sqlc.Queries, a OrdersCatalogChange) (OrdersCatalog, error) {
	var result OrdersCatalog
	raw, err := json.Marshal(a.Document)
	if err != nil {
		return result, err
	}
	var row sqlc.ShuttleOrdersCatalog
	if a.ExpectedVersion == 0 {
		row, err = q.InsertOrdersCatalog(ctx, sqlc.InsertOrdersCatalogParams{EventID: a.EventID, Document: raw})
	} else {
		row, err = q.UpdateOrdersCatalog(
			ctx,
			sqlc.UpdateOrdersCatalogParams{EventID: a.EventID, Document: raw, Version: a.ExpectedVersion},
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrConflict
	}
	if err != nil {
		return result, err
	}
	return ordersCatalogRecord(row)
}

func (s *Store) OrdersCatalogOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (OrdersCatalog, error) {
	var result OrdersCatalog
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return result, err
	}
	row, err := s.q.WithTx(tx).
		ReadOrdersCatalogOperation(ctx, sqlc.ReadOrdersCatalogOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(row.Result, &result)
	return result, err
}
