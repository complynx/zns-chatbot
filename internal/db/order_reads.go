package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func (s *Store) OwnOrder(ctx context.Context, scope ProfileScope, event, order uuid.UUID) (SubmittedOrder, error) {
	return s.ownOrderForKind(ctx, scope, event, order, OrderKindService)
}

func (s *Store) ownOrderForKind(ctx context.Context, scope ProfileScope, event, order uuid.UUID,
	kind OrderKind,
) (SubmittedOrder, error) {
	tx, err := s.orderReadTx(ctx, scope, event, kind)
	if err != nil {
		return SubmittedOrder{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnSubmittedOrder(ctx, s.q.WithTx(tx), scope.UserID, event, order, kind)
}

func readOwnSubmittedOrder(ctx context.Context, q *sqlc.Queries, owner, event, order uuid.UUID,
	kind OrderKind,
) (SubmittedOrder, error) {
	row, err := q.ReadSubmittedOrder(ctx, sqlc.ReadSubmittedOrderParams{
		OrderKind: string(kind), ID: order, UserID: owner, EventID: event,
	})
	if err != nil {
		return SubmittedOrder{}, err
	}
	return submittedOrderRecordForKind(row, kind)
}

func (s *Store) ListOwnOrders(
	ctx context.Context,
	scope ProfileScope,
	event, after uuid.UUID,
	limit int32,
) (OrdersPage, error) {
	return s.listOwnOrdersForKind(ctx, scope, event, after, limit, OrderKindService)
}

func (s *Store) listOwnOrdersForKind(ctx context.Context, scope ProfileScope, event, after uuid.UUID,
	limit int32, kind OrderKind,
) (OrdersPage, error) {
	if limit < 1 || limit > 20 {
		return OrdersPage{}, orders.ErrSelection
	}
	tx, err := s.orderReadTx(ctx, scope, event, kind)
	if err != nil {
		return OrdersPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnOrdersPage(ctx, s.q.WithTx(tx), scope.UserID, event, after, limit, kind)
}

func readOwnOrdersPage(ctx context.Context, q *sqlc.Queries, owner, event, after uuid.UUID,
	limit int32, kind OrderKind,
) (OrdersPage, error) {
	rows, err := q.ListOwnSubmittedOrders(ctx, sqlc.ListOwnSubmittedOrdersParams{
		OrderKind: string(kind), UserID: owner, EventID: event, AfterID: after, PageLimit: limit + 1,
	})
	if err != nil {
		return OrdersPage{}, err
	}
	result := OrdersPage{Orders: []SubmittedOrder{}}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		order, decodeErr := submittedOrderRecordForKind(row, kind)
		if decodeErr != nil {
			return OrdersPage{}, decodeErr
		}
		result.Orders = append(result.Orders, order)
	}
	if more {
		result.Next = &result.Orders[len(result.Orders)-1].ID
	}
	return result, nil
}

// OwnOrderOperation recovers one exact phase, after checking current access.
func (s *Store) OwnOrderOperation(
	ctx context.Context,
	scope ProfileScope,
	event, operation uuid.UUID,
	kind OrderOperationKind,
) (OrderOperation, error) {
	return s.ownOrderOperationForKind(ctx, scope, event, operation, kind, OrderKindService)
}

func (s *Store) ownOrderOperationForKind(ctx context.Context, scope ProfileScope, event, operation uuid.UUID,
	kind OrderOperationKind, orderKind OrderKind,
) (OrderOperation, error) {
	if operation == uuid.Nil || !kind.valid() {
		return OrderOperation{}, orders.ErrSelection
	}
	tx, err := s.orderReadTx(ctx, scope, event, orderKind)
	if err != nil {
		return OrderOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOrderOperationForKind(ctx, s.q.WithTx(tx), scope.UserID, event, operation, kind, orderKind)
}

func readOrderOperationForKind(
	ctx context.Context,
	q *sqlc.Queries,
	owner, event, operation uuid.UUID,
	kind OrderOperationKind,
	orderKind OrderKind,
) (OrderOperation, error) {
	rows, err := q.ReadOrderOperations(ctx, sqlc.ReadOrderOperationsParams{UserID: owner, OperationID: operation})
	if err != nil {
		return OrderOperation{}, err
	}
	for _, row := range rows {
		if row.OrderKind == string(orderKind) && row.Kind == string(kind) &&
			(event == uuid.Nil || row.EventID == event) {
			return OrderOperation{OperationID: operation, EventID: row.EventID, Kind: kind, Result: row.Result}, nil
		}
	}
	return OrderOperation{}, pgx.ErrNoRows
}

// OwnOrderOperationByID resolves the event under an owner lock, then releases
// it before taking the normal event -> owner locks. No event hint is trusted.
func (s *Store) OwnOrderOperationByID(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	kind OrderOperationKind,
) (OrderOperation, error) {
	return s.ownOrderOperationByIDForKind(ctx, scope, operation, kind, OrderKindService)
}

func (s *Store) ownOrderOperationByIDForKind(ctx context.Context, scope ProfileScope, operation uuid.UUID,
	kind OrderOperationKind, orderKind OrderKind,
) (OrderOperation, error) {
	if operation == uuid.Nil || !kind.valid() {
		return OrderOperation{}, orders.ErrSelection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return OrderOperation{}, err
	}
	receipt, err := readOrderOperationForKind(ctx, s.q.WithTx(tx), scope.UserID, uuid.Nil, operation, kind, orderKind)
	if err != nil {
		return OrderOperation{}, err
	}
	rollbackLegacyArchive(ctx, tx)
	return s.ownOrderOperationForKind(ctx, scope, receipt.EventID, operation, kind, orderKind)
}

// Read paths select a fixed business kind before acquiring module entitlement.
func (s *Store) orderReadTx(ctx context.Context, scope ProfileScope, event uuid.UUID, kind OrderKind) (pgx.Tx, error) {
	switch kind {
	case OrderKindService:
		return s.orderTx(ctx, scope, event, uuid.Nil)
	case OrderKindMeal:
		return s.mealOrderTx(ctx, scope, event, uuid.Nil)
	default:
		return nil, orders.ErrSelection
	}
}
