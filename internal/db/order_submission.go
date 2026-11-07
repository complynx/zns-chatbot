package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// SubmitOwnOrder atomically saves the approved snapshot, consumes the draft and
// records its immutable receipt. It does not reserve capacity or confirm money.
func (s *Store) SubmitOwnOrder(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args OrderSubmission,
	proposal uuid.UUID,
	source OrderSource,
) (OrderSubmissionResult, error) {
	call, err := orderSubmitCall(operation, args, proposal, source)
	if err != nil {
		return OrderSubmissionResult{}, err
	}
	tx, err := s.orderTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return OrderSubmissionResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.submitOrder(ctx, tx, scope, call, args, proposal)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func orderSubmitCall(
	operation uuid.UUID,
	args OrderSubmission,
	proposal uuid.UUID,
	source OrderSource,
) (orderCall, error) {
	if operation == uuid.Nil || proposal == uuid.Nil || !args.valid() || !source.valid() {
		return orderCall{}, orders.ErrSelection
	}
	business, err := orderHash(args)
	if err != nil {
		return orderCall{}, err
	}
	request, err := orderHash(struct {
		Arguments  OrderSubmission `json:"arguments"`
		ProposalID uuid.UUID       `json:"proposal_id"`
	}{args, proposal})
	return orderCall{
		event:        args.EventID,
		operation:    operation,
		kind:         OrderSubmit,
		source:       source,
		businessHash: business,
		requestHash:  request,
	}, err
}

// submitOrder uses the caller's event and owner locks and never commits.
func (s *Store) submitOrder(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	call orderCall,
	args OrderSubmission,
	proposal uuid.UUID,
) (OrderSubmissionResult, bool, error) {
	q := s.q.WithTx(tx)
	raw, err := orderReceipt(ctx, q, scope, call)
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	if raw != nil {
		var prior OrderSubmissionResult
		err = json.Unmarshal(raw, &prior)
		return prior, true, err
	}
	row, err := q.ReadOrderPreparation(
		ctx,
		sqlc.ReadOrderPreparationParams{
			OrderKind: string(call.businessKind()),
			ID:        proposal,
			UserID:    scope.UserID,
			EventID:   args.EventID,
		},
	)
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	if !bytes.Equal(row.RequestHash, call.businessHash) {
		return OrderSubmissionResult{}, false, ErrConflict
	}
	canonical, err := q.ReadCanonicalOrder(
		ctx,
		sqlc.ReadCanonicalOrderParams{OrderKind: string(call.businessKind()),
			UserID:             scope.UserID,
			EventID:            args.EventID,
			SourceDraftVersion: args.ExpectedDraftVersion,
		},
	)
	if err == nil {
		if !bytes.Equal(canonical.RequestHash, call.businessHash) {
			return OrderSubmissionResult{}, false, ErrConflict
		}
		var result OrderSubmissionResult
		if err = json.Unmarshal(canonical.SubmittedResult, &result); err == nil {
			err = insertOrderReceipt(ctx, q, scope, call, proposal, canonical.ID, result)
		}
		return result, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderSubmissionResult{}, false, err
	}
	prepared, err := orderPreparationRecordForKind(row, call.businessKind())
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	snapshot, now, err := s.orderSnapshotForKind(ctx, tx, scope, args, call.businessKind())
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	if row.ConsumedAt.Valid || !prepared.ExpiresAt.After(now) {
		return OrderSubmissionResult{}, false, ErrConflict
	}
	// Observation time changes between preparation and execution; all approved
	// business fields, prices and the original cutoff must remain identical.
	snapshot.Quote.ObservedAt = prepared.Snapshot.Quote.ObservedAt
	current, err := orderJSON(snapshot)
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	approved, err := orderJSON(prepared.Snapshot)
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	if !bytes.Equal(current, approved) {
		return OrderSubmissionResult{}, false, ErrConflict
	}
	result, err := storeSubmittedOrder(ctx, q, scope, call, args, prepared, now)
	return result, false, err
}

func storeSubmittedOrder(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	call orderCall,
	args OrderSubmission,
	prepared OrderPreparation,
	now time.Time,
) (OrderSubmissionResult, error) {
	result := OrderSubmissionResult{
		Order: SubmittedOrder{
			ID:                 uuid.New(),
			EventID:            args.EventID,
			SourceDraftVersion: args.ExpectedDraftVersion,
			Version:            1,
			State:              "submitted",
			Snapshot:           prepared.Snapshot,
			SubmittedAt:        now,
		},
		Selection: &OrdersSelection{
			EventID:        args.EventID,
			EventVersion:   args.ExpectedEventVersion,
			CatalogVersion: args.ExpectedCatalogVersion,
			Version:        args.ExpectedDraftVersion + 1,
			Selections:     []orders.Selection{},
			SavedAt:        &now,
		},
	}
	if call.businessKind() == OrderKindMeal {
		result.Order.OrderKind = OrderKindMeal
		result.Selection = nil
		result.MealDraft = &MealDraft{
			EventID: args.EventID, EventVersion: args.ExpectedEventVersion,
			CatalogVersion: args.ExpectedCatalogVersion, Version: args.ExpectedDraftVersion + 1,
			Timezone: prepared.Snapshot.Quote.Timezone, Cleared: true, SavedAt: &now,
		}
	}
	raw, err := orderJSON(result)
	if err != nil {
		return result, err
	}
	err = q.InsertSubmittedOrder(
		ctx,
		sqlc.InsertSubmittedOrderParams{OrderKind: string(call.businessKind()),
			ID:                 result.Order.ID,
			UserID:             scope.UserID,
			EventID:            args.EventID,
			SourceDraftVersion: args.ExpectedDraftVersion,
			PreparationID:      prepared.ID,
			RequestHash:        call.businessHash,
			SubmittedResult:    raw,
			SalesClosesAt:      pgtype.Timestamptz{Time: prepared.Snapshot.SalesClosesAt, Valid: true},
			SubmittedAt:        pgtype.Timestamptz{Time: now, Valid: true},
		},
	)
	if err != nil {
		return result, err
	}
	if err = saveConsumedOrderDraft(ctx, q, scope, args.EventID, result); err != nil {
		return result, err
	}
	consumed, err := q.ConsumeOrderPreparation(
		ctx,
		sqlc.ConsumeOrderPreparationParams{OrderKind: string(call.businessKind()),
			ObservedAt: pgtype.Timestamptz{Time: now, Valid: true},
			ID:         prepared.ID,
			UserID:     scope.UserID,
			EventID:    args.EventID,
		},
	)
	if err != nil {
		return result, err
	}
	if consumed != 1 {
		return result, ErrConflict
	}
	return result, insertOrderReceipt(ctx, q, scope, call, prepared.ID, result.Order.ID, result)
}

func saveConsumedOrderDraft(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	event uuid.UUID,
	result OrderSubmissionResult,
) error {
	if result.MealDraft != nil {
		raw, err := orderJSON(result.MealDraft)
		if err != nil {
			return err
		}
		return q.SaveMealDraft(
			ctx,
			sqlc.SaveMealDraftParams{
				UserID:         scope.UserID,
				EventID:        event,
				CatalogVersion: result.MealDraft.CatalogVersion,
				Version:        result.MealDraft.Version,
				Result:         raw,
			},
		)
	}
	raw, err := orderJSON(result.Selection)
	if err != nil {
		return err
	}
	return q.SaveOrdersSelection(
		ctx,
		sqlc.SaveOrdersSelectionParams{
			UserID:  scope.UserID,
			EventID: event,
			Version: result.Selection.Version,
			Result:  raw,
		},
	)
}
