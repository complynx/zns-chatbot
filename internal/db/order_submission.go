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
		sqlc.ReadOrderPreparationParams{ID: proposal, UserID: scope.UserID, EventID: args.EventID},
	)
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	if !bytes.Equal(row.RequestHash, call.businessHash) {
		return OrderSubmissionResult{}, false, ErrConflict
	}
	canonical, err := q.ReadCanonicalOrder(
		ctx,
		sqlc.ReadCanonicalOrderParams{
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
	prepared, err := orderPreparationRecord(row)
	if err != nil {
		return OrderSubmissionResult{}, false, err
	}
	snapshot, now, err := s.orderSnapshot(ctx, tx, scope, args)
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
		Selection: OrdersSelection{
			EventID:        args.EventID,
			EventVersion:   args.ExpectedEventVersion,
			CatalogVersion: args.ExpectedCatalogVersion,
			Version:        args.ExpectedDraftVersion + 1,
			Selections:     []orders.Selection{},
			SavedAt:        &now,
		},
	}
	raw, err := orderJSON(result)
	if err != nil {
		return result, err
	}
	err = q.InsertSubmittedOrder(
		ctx,
		sqlc.InsertSubmittedOrderParams{
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
	draft, err := orderJSON(result.Selection)
	if err != nil {
		return result, err
	}
	err = q.SaveOrdersSelection(
		ctx,
		sqlc.SaveOrdersSelectionParams{
			UserID:  scope.UserID,
			EventID: args.EventID,
			Version: result.Selection.Version,
			Result:  draft,
		},
	)
	if err != nil {
		return result, err
	}
	consumed, err := q.ConsumeOrderPreparation(
		ctx,
		sqlc.ConsumeOrderPreparationParams{
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
