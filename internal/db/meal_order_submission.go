package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// mealOrderTx shares operation IDs and event-owner locking with service orders,
// but checks the independent meals module before any receipt replay.
func (s *Store) mealOrderTx(ctx context.Context, scope ProfileScope, event, operation uuid.UUID) (pgx.Tx, error) {
	tx, _, err := s.mealEventTx(ctx, scope, event, operation, orderModuleServices)
	if err != nil {
		return nil, err
	}
	if err = authorizeMealDraft(ctx, tx, scope, event); err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}

// PrepareOwnMealOrderSubmission freezes the saved meal draft without creating
// an order or accepting payment. Empty composition returns ErrEmptyMealOrder.
func (s *Store) PrepareOwnMealOrderSubmission(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, args OrderSubmission, source OrderSource,
) (OrderPreparation, error) {
	call, err := orderPrepareCall(operation, args, source)
	if err != nil {
		return OrderPreparation{}, err
	}
	call.orderKind = OrderKindMeal
	tx, err := s.mealOrderTx(ctx, scope, args.EventID, operation)
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

// SubmitOwnMealOrder uses the shared canonical order and operation receipts. It
// consumes only the meal draft and creates no money, capacity hold or evidence.
func (s *Store) SubmitOwnMealOrder(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, args OrderSubmission, proposal uuid.UUID, source OrderSource,
) (OrderSubmissionResult, error) {
	call, err := orderSubmitCall(operation, args, proposal, source)
	if err != nil {
		return OrderSubmissionResult{}, err
	}
	call.orderKind = OrderKindMeal
	tx, err := s.mealOrderTx(ctx, scope, args.EventID, operation)
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

// CancelOwnMealOrder retains the original meal terms and submission receipt.
func (s *Store) CancelOwnMealOrder(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, args OrderCancellation, source OrderSource,
) (SubmittedOrder, error) {
	call, err := orderCancelCall(operation, args, source)
	if err != nil {
		return SubmittedOrder{}, err
	}
	call.orderKind = OrderKindMeal
	tx, err := s.mealOrderTx(ctx, scope, args.EventID, operation)
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
