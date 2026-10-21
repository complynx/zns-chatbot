package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

type orderCall struct {
	event, operation          uuid.UUID
	orderKind                 OrderKind
	kind                      OrderOperationKind
	source                    OrderSource
	businessHash, requestHash []byte
}

// businessKind keeps existing service callers explicit at the shared receipt boundary.
func (c orderCall) businessKind() OrderKind {
	if c.orderKind == "" {
		return OrderKindService
	}
	return c.orderKind
}

// orderTx shares the selection's event -> owner serialization, without external IO.
func (s *Store) orderTx(ctx context.Context, scope ProfileScope, event, operation uuid.UUID) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	if operation != uuid.Nil {
		err = q.LockEventOperation(ctx, fmt.Sprintf("orders:%s:%s", scope.UserID, operation))
	}
	if err == nil {
		_, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
	if err == nil {
		err = authorizeOrdersSelection(ctx, tx, scope, event)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}

// Matching receipts precede freshness checks, but never current authorization.
func orderReceipt(ctx context.Context, q *sqlc.Queries, scope ProfileScope, call orderCall) ([]byte, error) {
	rows, err := q.ReadOrderOperations(
		ctx,
		sqlc.ReadOrderOperationsParams{UserID: scope.UserID, OperationID: call.operation},
	)
	if err != nil {
		return nil, err
	}
	var result []byte
	for _, row := range rows {
		if row.OrderKind != string(call.businessKind()) || row.EventID != call.event ||
			!bytes.Equal(row.BusinessHash, call.businessHash) ||
			(row.Kind == string(OrderCancel)) != (call.kind == OrderCancel) {
			return nil, ErrConflict
		}
		if row.Kind == string(call.kind) {
			if !bytes.Equal(row.RequestHash, call.requestHash) {
				return nil, ErrConflict
			}
			result = row.Result
		}
	}
	return result, nil
}

func insertOrderReceipt(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	call orderCall,
	preparation, order uuid.UUID,
	value any,
) error {
	raw, err := orderJSON(value)
	if err != nil {
		return err
	}
	return q.InsertOrderOperation(ctx, sqlc.InsertOrderOperationParams{OrderKind: string(call.businessKind()),
		UserID:      scope.UserID,
		OperationID: call.operation,
		Kind:        string(call.kind),
		EventID:     call.event,
		PreparationID: pgtype.UUID{
			Bytes: preparation,
			Valid: preparation != uuid.Nil,
		},
		OrderID:      pgtype.UUID{Bytes: order, Valid: order != uuid.Nil},
		BusinessHash: call.businessHash,
		RequestHash:  call.requestHash,
		Result:       raw,
		Source:       string(call.source),
	})
}

func submittedOrderRecordForKind(row sqlc.ShuttleOrder, kind OrderKind) (SubmittedOrder, error) {
	if row.OrderKind != string(kind) {
		return SubmittedOrder{}, ErrConflict
	}
	var original OrderSubmissionResult
	if err := json.Unmarshal(row.SubmittedResult, &original); err != nil {
		return SubmittedOrder{}, err
	}
	order := original.Order
	order.Version, order.State = row.Version, row.State
	if row.CancelledAt.Valid {
		when := row.CancelledAt.Time.UTC()
		order.CancelledAt = &when
	}
	return order, nil
}
