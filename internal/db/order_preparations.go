package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// PrepareOwnOrderSubmission stores no order, capacity reservation or money.
func (s *Store) PrepareOwnOrderSubmission(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args OrderSubmission,
	source OrderSource,
) (OrderPreparation, error) {
	call, err := orderPrepareCall(operation, args, source)
	if err != nil {
		return OrderPreparation{}, err
	}
	tx, err := s.orderTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return OrderPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.prepareOrderSubmission(ctx, tx, scope, call, args)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// prepareOrderSubmission never commits or obtains a second connection.
func (s *Store) prepareOrderSubmission(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	call orderCall,
	args OrderSubmission,
) (OrderPreparation, bool, error) {
	q := s.q.WithTx(tx)
	raw, err := orderReceipt(ctx, q, scope, call)
	if err != nil {
		return OrderPreparation{}, false, err
	}
	if raw != nil {
		var prior OrderPreparation
		err = json.Unmarshal(raw, &prior)
		return prior, true, err
	}
	snapshot, now, err := s.orderSnapshot(ctx, tx, scope, args)
	if err != nil {
		return OrderPreparation{}, false, err
	}
	row, err := q.FindPendingOrderPreparation(
		ctx,
		sqlc.FindPendingOrderPreparationParams{
			UserID:             scope.UserID,
			EventID:            args.EventID,
			SourceDraftVersion: args.ExpectedDraftVersion,
			RequestHash:        call.businessHash,
			ObservedAt:         pgtype.Timestamptz{Time: now, Valid: true},
		},
	)
	var result OrderPreparation
	if err == nil {
		result, err = orderPreparationRecord(row)
		if err == nil {
			var same bool
			same, err = sameOrderConditions(snapshot, result.Snapshot)
			if err == nil && !same {
				result, err = insertOrderPreparation(ctx, q, scope, args, snapshot, now, call.businessHash)
			}
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		result, err = insertOrderPreparation(ctx, q, scope, args, snapshot, now, call.businessHash)
	}
	if err == nil {
		err = insertOrderReceipt(ctx, q, scope, call, result.ID, uuid.Nil, result)
	}
	return result, false, err
}

func orderPreparationRecord(row sqlc.ShuttleOrderPreparation) (OrderPreparation, error) {
	if row.OrderKind != string(OrderKindService) {
		return OrderPreparation{}, ErrConflict
	}
	p := OrderPreparation{ID: row.ID, ExpiresAt: row.ExpiresAt.Time.UTC()}
	if err := json.Unmarshal(row.Arguments, &p.Arguments); err != nil {
		return p, err
	}
	return p, json.Unmarshal(row.Snapshot, &p.Snapshot)
}

func insertOrderPreparation(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	args OrderSubmission,
	snapshot OrderSnapshot,
	now time.Time,
	hash []byte,
) (OrderPreparation, error) {
	expiry := now.Add(assignmentPreparationTTL)
	if snapshot.SalesClosesAt.Before(expiry) {
		expiry = snapshot.SalesClosesAt
	}
	result := OrderPreparation{ID: uuid.New(), Arguments: args, Snapshot: snapshot, ExpiresAt: expiry.UTC()}
	rawArgs, err := orderJSON(args)
	if err != nil {
		return result, err
	}
	raw, err := orderJSON(snapshot)
	if err != nil {
		return result, err
	}
	err = q.InsertOrderPreparation(
		ctx,
		sqlc.InsertOrderPreparationParams{
			ID:                 result.ID,
			UserID:             scope.UserID,
			EventID:            args.EventID,
			SourceDraftVersion: args.ExpectedDraftVersion,
			RequestHash:        hash,
			Arguments:          rawArgs,
			Snapshot:           raw,
			CreatedAt:          pgtype.Timestamptz{Time: now, Valid: true},
			ExpiresAt:          pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
		},
	)
	return result, err
}

// orderSnapshot recalculates only the locked current saved draft, using database time.
func (s *Store) orderSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	args OrderSubmission,
) (OrderSnapshot, time.Time, error) {
	catalog, err := s.ownOrdersCatalogSnapshot(ctx, tx, scope, args.EventID)
	if err != nil {
		return OrderSnapshot{}, time.Time{}, err
	}
	now := catalog.ObservedAt.UTC()
	if catalog.EventVersion != args.ExpectedEventVersion || catalog.Version != args.ExpectedCatalogVersion {
		return OrderSnapshot{}, now, ErrConflict
	}
	draft, err := readOrdersSelection(ctx, s.q.WithTx(tx), scope.UserID, args.EventID)
	if err != nil {
		return OrderSnapshot{}, now, err
	}
	if draft.Version != args.ExpectedDraftVersion || draft.Version == math.MaxInt64 ||
		draft.EventVersion != args.ExpectedEventVersion ||
		draft.CatalogVersion != args.ExpectedCatalogVersion ||
		len(draft.Selections) == 0 ||
		draft.Quote == nil {
		return OrderSnapshot{}, now, ErrConflict
	}
	document := orders.CatalogDocument{
		Currency:         catalog.Currency,
		CurrencyScale:    catalog.CurrencyScale,
		Items:            catalog.Items,
		CompletePricing:  catalog.CompletePricing,
		CompleteCapacity: catalog.CompleteCapacity,
	}
	for _, offer := range catalog.Offers {
		document.Offers = append(document.Offers, offer.Offer)
	}
	quote, err := orders.Calculate(document, draft.Selections, now)
	if err != nil {
		return OrderSnapshot{}, now, err
	}
	capacity, err := orders.CapacityRequirements(document, draft.Selections)
	if err != nil {
		return OrderSnapshot{}, now, err
	}
	snapshot := OrderSnapshot{
		Capacity:   capacity,
		EventName:  catalog.EventName,
		Selections: append([]orders.Selection{}, draft.Selections...),
		Quote: OrdersQuoteView{
			Quote:        quote,
			EventID:      args.EventID,
			EventVersion: catalog.EventVersion,
			Version:      catalog.Version,
			Timezone:     catalog.Timezone,
			ObservedAt:   now,
		},
	}
	selectedOffers := make(map[string]bool, len(draft.Selections))
	for _, selection := range draft.Selections {
		selectedOffers[selection.OfferID] = true
	}
	for _, offer := range document.Offers {
		if !selectedOffers[offer.ID] {
			continue
		}
		snapshot.Services = append(snapshot.Services, OrderService{
			OfferID: offer.ID, Name: offer.Name,
			StartsAt: offer.StartsAt.UTC(), EndsAt: offer.EndsAt.UTC(),
		})
		if snapshot.SalesClosesAt.IsZero() || offer.SalesClosesAt.Before(snapshot.SalesClosesAt) {
			snapshot.SalesClosesAt = offer.SalesClosesAt.UTC()
		}
	}
	return snapshot, now, nil
}

// sameOrderConditions ignores only the observation timestamp, not sold terms.
func sameOrderConditions(current, approved OrderSnapshot) (bool, error) {
	current.Quote.ObservedAt = approved.Quote.ObservedAt
	raw, err := orderJSON(current)
	if err != nil {
		return false, err
	}
	prior, err := orderJSON(approved)
	return bytes.Equal(raw, prior), err
}
