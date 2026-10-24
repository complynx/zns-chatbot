package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/orders"
)

// MiniAppOwnMealOrder validates the current bearer inside the meal transaction,
// including receipt recovery after waiting for the event lock.
func (s *Store) MiniAppOwnMealOrder(ctx context.Context, deployment EventScope, session MiniAppOrderSession,
	event, order uuid.UUID,
) (SubmittedOrder, error) {
	tx, scope, err := s.miniAppMealOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return SubmittedOrder{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnSubmittedOrder(ctx, s.q.WithTx(tx), scope.UserID, event, order, OrderKindMeal)
}

func (s *Store) MiniAppOwnMealOrders(ctx context.Context, deployment EventScope, session MiniAppOrderSession,
	event, after uuid.UUID, limit int32,
) (OrdersPage, error) {
	if limit < 1 || limit > 20 {
		return OrdersPage{}, orders.ErrSelection
	}
	tx, scope, err := s.miniAppMealOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return OrdersPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnOrdersPage(ctx, s.q.WithTx(tx), scope.UserID, event, after, limit, OrderKindMeal)
}

func (s *Store) MiniAppOwnMealOrderOperation(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, event, operation uuid.UUID, kind OrderOperationKind,
) (OrderOperation, error) {
	if operation == uuid.Nil || !kind.valid() {
		return OrderOperation{}, orders.ErrSelection
	}
	tx, scope, err := s.miniAppMealOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return OrderOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOrderOperationForKind(ctx, s.q.WithTx(tx), scope.UserID, event, operation, kind, OrderKindMeal)
}
