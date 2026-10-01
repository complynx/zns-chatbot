package orders

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/orders/dbgen"
)

// RouteRefunds durably requests a manual refund from the current pass ambassador.
// Unassigned obligations remain pending and are retried without blocking other owners.
func (s Service) RouteRefunds(ctx context.Context) (int, error) {
	if err := s.Delivery.Validate(); err != nil {
		return 0, err
	}
	rows, err := s.DB.Query(ctx, `SELECT id FROM core.order_refund_tasks WHERE state='pending'
 AND next_route_at<=clock_timestamp() ORDER BY next_route_at,id LIMIT $1`, refundPageSize)
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	count := 0
	for _, id := range ids {
		routed, routeErr := s.routeRefund(ctx, id)
		if routeErr != nil {
			return count, routeErr
		}
		if routed {
			count++
		}
	}
	return count, nil
}

func (s Service) routeRefund(ctx context.Context, id int64) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	task, err := lockRefund(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if task.State != refundPending {
		return false, nil
	}
	if err = lockRefundAmbassador(ctx, tx, task); err != nil {
		return false, err
	}
	ambassador, reason, err := currentRefundAmbassador(ctx, tx, task.EventID, task.Owner)
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE core.order_refund_tasks SET next_route_at=clock_timestamp()+interval '1 minute' WHERE id=$1`,
		id,
	); err != nil {
		return false, core.DatabaseOperationError(err)
	}
	evidenceID, err := refundRequestEvidence(ctx, tx, task.ID, ambassador)
	if err != nil {
		return false, err
	}
	var evidence *int64
	if evidenceID != 0 {
		evidence = &evidenceID
	}
	current, err := refundRouteCurrent(ctx, tx, task, ambassador, reason, evidence)
	if err != nil {
		return false, err
	}
	if current {
		return false, core.DatabaseOperationError(tx.Commit(ctx))
	}
	task.Version++
	notification := evidence
	routed := ambassador != "" && notification == nil
	if routed {
		notice, enqueueErr := s.enqueueRefund(ctx, tx, task, ambassador)
		if enqueueErr != nil {
			return false, enqueueErr
		}
		notification = &notice
	}
	_, err = tx.Exec(ctx, `UPDATE core.order_refund_tasks SET ambassador=NULLIF($2,''),routing_reason=$3,
 version=$4,notification_id=COALESCE($5,notification_id) WHERE id=$1`, id, ambassador, reason, task.Version, notification)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if err = auditRefund(ctx, tx, id, task.Owner, "routed", task.Version); err != nil {
		return false, err
	}
	return routed, core.DatabaseOperationError(tx.Commit(ctx))
}

// A recipient's known or possible send survives routing loss, chat changes and reassignment.
// Zero means that no known or possible send exists for the recipient.
func refundRequestEvidence(ctx context.Context, tx pgx.Tx, id int64, ambassador string) (int64, error) {
	if ambassador == "" {
		return 0, nil
	}
	var notification int64
	err := tx.QueryRow(ctx, `SELECT id FROM core.order_notifications
 WHERE payload->>'kind'='refund_request' AND payload->>'refund_id'=$1 AND recipient=$2
 AND (delivery_state IN ('sent','sending','unknown') OR last_uncertain_attempt IS NOT NULL)
 ORDER BY id LIMIT 1`, strconv.FormatInt(id, 10), ambassador).Scan(&notification)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	return notification, nil
}

func refundRouteCurrent(
	ctx context.Context,
	tx pgx.Tx,
	task RefundTask,
	ambassador, reason string,
	evidence *int64,
) (bool, error) {
	if task.Ambassador != ambassador || task.RoutingReason != reason {
		return false, nil
	}
	if ambassador == "" {
		return true, nil
	}
	var current bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_refund_tasks t
 JOIN core.order_notifications n ON n.id=t.notification_id JOIN core.users u ON u.id=$2
 WHERE t.id=$1 AND (($3::bigint IS NOT NULL AND n.id=$3) OR
 ($3::bigint IS NULL AND n.delivery_chat=u.telegram_id AND n.delivery_state NOT IN ('cancelled','failed'))))`, task.ID, ambassador, evidence).Scan(&current)
	return current, core.DatabaseOperationError(err)
}

func (s Service) enqueueRefund(ctx context.Context, tx pgx.Tx, task RefundTask, ambassador string) (int64, error) {
	payload, err := json.Marshal(Notification{Recipient: ambassador, EventID: task.EventID, OrderID: task.OrderID,
		Kind: refundRequest, RefundID: task.ID, RefundVersion: task.Version})
	if err != nil {
		return 0, err
	}
	row, err := dbgen.New(tx).EnqueueNotification(ctx, dbgen.EnqueueNotificationParams{
		Recipient: ambassador, OrderID: task.OrderID, Payload: payload, BotID: s.Delivery.BotID})
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	var registrations []delivery.Registration
	if err = collectNotificationRegistration(
		ctx,
		tx,
		s.Delivery.BotID,
		row.ID,
		row.DeliveryChat,
		&registrations,
	); err != nil {
		return 0, err
	}
	if err = delivery.RegisterBatch(ctx, tx, s.Delivery.BotID, registrations); err != nil {
		return 0, err
	}
	return row.ID, nil
}
