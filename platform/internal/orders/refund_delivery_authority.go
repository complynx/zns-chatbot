package orders

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"
)

// RefundDeliveryRead binds the mutable task and current reader projection.
type RefundDeliveryRead struct {
	ID            int64  `json:"id"`
	Version       int64  `json:"version"`
	Ambassador    string `json:"ambassador"`
	RoutingReason string `json:"routing_reason"`
	CanConfirm    bool   `json:"can_confirm"`
}

func (task RefundTask) DeliveryRead() RefundDeliveryRead {
	return RefundDeliveryRead{
		ID: task.ID, Version: task.Version, Ambassador: task.Ambassador,
		RoutingReason: task.RoutingReason, CanConfirm: task.CanConfirm,
	}
}

// RefundDeliveryActorInTx runs after event locks and before sorted actor locks.
// Include the raw assignee even when their unavailable status hides their identity in the projection.
func RefundDeliveryActorInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, event string,
	expected RefundDeliveryRead,
) (string, error) {
	task, err := scanRefund(tx.QueryRow(ctx, `SELECT `+refundColumns+`
 FROM core.order_refund_tasks t WHERE t.id=$1`, expected.ID))
	if err != nil {
		return "", refundReadError(err)
	}
	if err = checkRefundDelivery(ctx, tx, actor, event, expected, task); err != nil {
		return "", err
	}
	var ambassador string
	err = tx.QueryRow(ctx, `SELECT payment_admin FROM core.pass_bookings WHERE event_id=$1 AND owner=$2`,
		event, task.Owner).Scan(&ambassador)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return ambassador, core.DatabaseOperationError(err)
}

// LockDeliveryRefundInTx retains current read authority through wire admission.
// The caller holds the pass/order event locks and the recipient's user row first.
func LockDeliveryRefundInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, event, lockedAmbassador string,
	expected RefundDeliveryRead,
) error {
	if expected.ID <= 0 || expected.Version <= 0 {
		return refundInvalid()
	}
	task, err := scanRefund(tx.QueryRow(ctx, `SELECT `+refundColumns+`
 FROM core.order_refund_tasks t WHERE t.id=$1 FOR SHARE`, expected.ID))
	if err != nil {
		return refundReadError(err)
	}
	if task.EventID != event || task.Version != expected.Version {
		return problem(http.StatusConflict, "refund_stale")
	}
	var ambassador string
	err = tx.QueryRow(ctx, `SELECT payment_admin FROM core.pass_bookings WHERE event_id=$1 AND owner=$2 FOR SHARE`,
		event, task.Owner).Scan(&ambassador)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationError(err)
	}
	if ambassador != lockedAmbassador {
		return problem(http.StatusConflict, "refund_stale")
	}
	if err = lockRefundAmbassador(ctx, tx, task); err != nil {
		return err
	}
	var globalAdmin string
	err = tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).
		Scan(&globalAdmin)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationError(err)
	}
	return checkRefundDelivery(ctx, tx, actor, event, expected, task)
}

func checkRefundDelivery(
	ctx context.Context,
	tx pgx.Tx,
	actor, event string,
	expected RefundDeliveryRead,
	task RefundTask,
) error {
	if task.EventID != event || task.Version != expected.Version {
		return problem(http.StatusConflict, "refund_stale")
	}
	if err := authorizeRefundRead(ctx, tx, actor, &task); err != nil {
		return err
	}
	if task.DeliveryRead() != expected {
		return problem(http.StatusConflict, "refund_stale")
	}
	return nil
}
