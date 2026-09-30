package orders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"
)

// ConfirmRefund records the current ambassador's explicit confirmation of a completed manual refund.
func (s Service) ConfirmRefund(ctx context.Context, actor string, c RefundConfirmation) (RefundTask, error) {
	if c.ID <= 0 || c.Version <= 0 || !c.Confirmed || strings.TrimSpace(c.Key) == "" || len(c.Key) > maxKeyLength {
		return RefundTask{}, refundInvalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return RefundTask{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	task, err := lockRefund(ctx, tx, c.ID)
	if err != nil {
		return RefundTask{}, err
	}
	if err = lockRefundAmbassador(ctx, tx, task); err != nil {
		return RefundTask{}, err
	}
	ambassador, _, err := currentRefundAmbassador(ctx, tx, task.EventID, task.Owner)
	if err != nil {
		return RefundTask{}, err
	}
	if ambassador == "" || actor != ambassador {
		return RefundTask{}, problem(http.StatusForbidden, "forbidden")
	}
	if err = confirmRefund(ctx, tx, actor, c, task); err != nil {
		return RefundTask{}, err
	}
	task, err = scanRefund(
		tx.QueryRow(ctx, `SELECT `+refundColumns+` FROM core.order_refund_tasks t WHERE t.id=$1`, c.ID),
	)
	if err != nil {
		return RefundTask{}, err
	}
	task.Ambassador, task.RoutingReason = ambassador, ""
	return task, core.DatabaseOperationError(tx.Commit(ctx))
}

func confirmRefund(ctx context.Context, tx pgx.Tx, actor string, c RefundConfirmation, task RefundTask) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	var previous string
	err = core.DatabaseOperationError(
		tx.QueryRow(ctx, `SELECT request_hash FROM core.order_refund_operations WHERE actor=$1 AND key=$2`, actor, c.Key).
			Scan(&previous),
	)
	if err == nil {
		if previous != hash {
			return problem(http.StatusConflict, "key_conflict")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if task.State != refundPending {
		return problem(http.StatusConflict, "refund_already_completed")
	}
	if task.Version != c.Version {
		return problem(http.StatusConflict, "refund_stale")
	}
	result, err := tx.Exec(ctx, `INSERT INTO core.order_refund_operations(actor,key,request_hash,task_id)
 VALUES($1,$2,$3,$4) ON CONFLICT(actor,key) DO NOTHING`, actor, c.Key, hash, c.ID)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if result.RowsAffected() == 0 {
		return problem(http.StatusConflict, "key_conflict")
	}
	_, err = tx.Exec(ctx, `UPDATE core.order_refund_tasks SET state='refunded',refunded_at=clock_timestamp(),
 refunded_by=$2,version=version+1 WHERE id=$1`, c.ID, actor)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return auditRefund(ctx, tx, c.ID, actor, "refunded", task.Version+1)
}

// Lock pass authority before the order event, matching pass mutation lock order.
func lockRefund(ctx context.Context, tx pgx.Tx, id int64) (RefundTask, error) {
	var event string
	err := tx.QueryRow(ctx, `SELECT event_id FROM core.order_refund_tasks WHERE id=$1`, id).Scan(&event)
	if err != nil {
		return RefundTask{}, refundReadError(core.DatabaseOperationError(err))
	}
	if err = lockRefundPassEvent(ctx, tx, event); err != nil {
		return RefundTask{}, err
	}
	if err = LockEvent(ctx, tx, event); err != nil {
		return RefundTask{}, err
	}
	task, err := scanRefund(
		tx.QueryRow(ctx, `SELECT `+refundColumns+` FROM core.order_refund_tasks t WHERE t.id=$1 FOR UPDATE`, id),
	)
	return task, refundReadError(err)
}

func lockRefundPassEvent(ctx context.Context, tx pgx.Tx, event string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM core.pass_events WHERE id=$1 FOR SHARE`, event).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return core.DatabaseOperationError(err)
}

// Hold current booking, consent and role rows through confirmation or wire admission.
func lockRefundAmbassador(ctx context.Context, tx pgx.Tx, task RefundTask) error {
	var ambassador string
	err := tx.QueryRow(ctx, `SELECT payment_admin FROM core.pass_bookings WHERE event_id=$1 AND owner=$2 FOR SHARE`, task.EventID, task.Owner).
		Scan(&ambassador)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || ambassador == "" {
		return core.DatabaseOperationError(err)
	}
	for _, statement := range []string{
		`SELECT id FROM core.users WHERE id=$1 FOR SHARE`,
		`SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`,
		`SELECT owner FROM core.pass_payment_admins WHERE owner=$1 AND event_id=$2 FOR SHARE`,
	} {
		args := []any{ambassador}
		if strings.Contains(statement, "$2") {
			args = append(args, task.EventID)
		}
		var locked string
		err = tx.QueryRow(ctx, statement, args...).Scan(&locked)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return core.DatabaseOperationError(err)
		}
	}
	return nil
}
