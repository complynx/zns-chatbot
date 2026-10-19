package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func (s *Store) OwnOrder(ctx context.Context, scope ProfileScope, event, order uuid.UUID) (SubmittedOrder, error) {
	tx, err := s.orderTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return SubmittedOrder{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		ReadSubmittedOrder(ctx, sqlc.ReadSubmittedOrderParams{ID: order, UserID: scope.UserID, EventID: event})
	if err != nil {
		return SubmittedOrder{}, err
	}
	return submittedOrderRecord(row)
}

func (s *Store) ListOwnOrders(
	ctx context.Context,
	scope ProfileScope,
	event, after uuid.UUID,
	limit int32,
) (OrdersPage, error) {
	if limit < 1 || limit > 20 {
		return OrdersPage{}, orders.ErrSelection
	}
	tx, err := s.orderTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return OrdersPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := s.q.WithTx(tx).
		ListOwnSubmittedOrders(ctx, sqlc.ListOwnSubmittedOrdersParams{UserID: scope.UserID, EventID: event, AfterID: after, PageLimit: limit + 1})
	if err != nil {
		return OrdersPage{}, err
	}
	result := OrdersPage{Orders: []SubmittedOrder{}}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		order, decodeErr := submittedOrderRecord(row)
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
	if operation == uuid.Nil || !kind.valid() {
		return OrderOperation{}, orders.ErrSelection
	}
	tx, err := s.orderTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return OrderOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOrderOperation(ctx, s.q.WithTx(tx), scope.UserID, event, operation, kind)
}

func readOrderOperation(
	ctx context.Context,
	q *sqlc.Queries,
	owner, event, operation uuid.UUID,
	kind OrderOperationKind,
) (OrderOperation, error) {
	rows, err := q.ReadOrderOperations(ctx, sqlc.ReadOrderOperationsParams{UserID: owner, OperationID: operation})
	if err != nil {
		return OrderOperation{}, err
	}
	for _, row := range rows {
		if row.OrderKind == string(OrderKindService) && row.Kind == string(kind) &&
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
	receipt, err := readOrderOperation(ctx, s.q.WithTx(tx), scope.UserID, uuid.Nil, operation, kind)
	if err != nil {
		return OrderOperation{}, err
	}
	rollbackLegacyArchive(ctx, tx)
	return s.OwnOrderOperation(ctx, scope, receipt.EventID, operation, kind)
}
