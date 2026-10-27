package db

import (
	"context"

	"github.com/google/uuid"
)

func (s *Store) OwnMealOrder(ctx context.Context, scope ProfileScope, event, order uuid.UUID) (SubmittedOrder, error) {
	return s.ownOrderForKind(ctx, scope, event, order, OrderKindMeal)
}

func (s *Store) ListOwnMealOrders(
	ctx context.Context,
	scope ProfileScope,
	event, after uuid.UUID,
	limit int32,
) (OrdersPage, error) {
	return s.listOwnOrdersForKind(ctx, scope, event, after, limit, OrderKindMeal)
}

func (s *Store) OwnMealOrderOperation(ctx context.Context, scope ProfileScope, event, operation uuid.UUID,
	kind OrderOperationKind,
) (OrderOperation, error) {
	return s.ownOrderOperationForKind(ctx, scope, event, operation, kind, OrderKindMeal)
}

func (s *Store) OwnMealOrderOperationByID(ctx context.Context, scope ProfileScope, operation uuid.UUID,
	kind OrderOperationKind,
) (OrderOperation, error) {
	return s.ownOrderOperationByIDForKind(ctx, scope, operation, kind, OrderKindMeal)
}
