package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// miniAppMealOrderTx uses the shared order namespace and event-owner-session
// locks, but requires meals entitlement. The API must check current IAM first.
func (s *Store) miniAppMealOrderTx(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, event, operation uuid.UUID,
) (pgx.Tx, ProfileScope, error) {
	return s.miniAppOrderTxForKind(ctx, deployment, session, event, operation, OrderKindMeal)
}

func (s *Store) PrepareMiniAppMealOrderSubmission(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, operation uuid.UUID, args OrderSubmission,
) (OrderPreparation, error) {
	call, err := orderPrepareCall(operation, args, OrderSourceMiniApp)
	if err != nil {
		return OrderPreparation{}, err
	}
	call.orderKind = OrderKindMeal
	tx, scope, err := s.miniAppMealOrderTx(ctx, deployment, session, args.EventID, operation)
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

func (s *Store) SubmitMiniAppMealOrder(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, operation uuid.UUID, args OrderSubmission, proposal uuid.UUID,
) (OrderSubmissionResult, error) {
	call, err := orderSubmitCall(operation, args, proposal, OrderSourceMiniApp)
	if err != nil {
		return OrderSubmissionResult{}, err
	}
	call.orderKind = OrderKindMeal
	tx, scope, err := s.miniAppMealOrderTx(ctx, deployment, session, args.EventID, operation)
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

func (s *Store) CancelMiniAppMealOrder(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, operation uuid.UUID, args OrderCancellation,
) (SubmittedOrder, error) {
	call, err := orderCancelCall(operation, args, OrderSourceMiniApp)
	if err != nil {
		return SubmittedOrder{}, err
	}
	call.orderKind = OrderKindMeal
	tx, scope, err := s.miniAppMealOrderTx(ctx, deployment, session, args.EventID, operation)
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
