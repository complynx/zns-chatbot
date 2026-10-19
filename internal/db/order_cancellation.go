package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// CancelOwnOrder retains the snapshot and original submission receipt. The
// frozen sales cutoff and expected current revision govern a new cancellation.
func (s *Store) CancelOwnOrder(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args OrderCancellation,
	source OrderSource,
) (SubmittedOrder, error) {
	call, err := orderCancelCall(operation, args, source)
	if err != nil {
		return SubmittedOrder{}, err
	}
	tx, err := s.orderTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return SubmittedOrder{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.cancelOrder(ctx, tx, scope, call, args)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) cancelOrder(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	call orderCall,
	args OrderCancellation,
) (SubmittedOrder, bool, error) {
	q := s.q.WithTx(tx)
	raw, err := orderReceipt(ctx, q, scope, call)
	if err != nil {
		return SubmittedOrder{}, false, err
	}
	if raw != nil {
		var prior SubmittedOrder
		err = json.Unmarshal(raw, &prior)
		return prior, true, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return SubmittedOrder{}, false, err
	}
	row, err := q.CancelSubmittedOrder(
		ctx,
		sqlc.CancelSubmittedOrderParams{
			ObservedAt:      now,
			ID:              args.OrderID,
			UserID:          scope.UserID,
			EventID:         args.EventID,
			ExpectedVersion: args.ExpectedVersion,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubmittedOrder{}, false, ErrConflict
	}
	if err != nil {
		return SubmittedOrder{}, false, err
	}
	result, err := submittedOrderRecord(row)
	if err == nil {
		err = insertOrderReceipt(ctx, q, scope, call, uuid.Nil, args.OrderID, result)
	}
	return result, false, err
}
