package orders

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"
)

const refundPending = "pending"
const refundRequest = "refund_request"
const refundPageSize = 25

// RefundTask records a manual refund obligation, never an instruction to transfer money automatically.
type RefundTask struct {
	ID                  int64            `json:"id"`
	Version             int64            `json:"version"`
	DisplacementVersion int64            `json:"displacement_version"`
	EventID             string           `json:"event_id"`
	OrderID             string           `json:"order_id"`
	Owner               string           `json:"owner"`
	PaymentAttempt      string           `json:"payment_attempt"`
	Amount              Money            `json:"amount"`
	Extras              map[string]Money `json:"extras"`
	Currency            string           `json:"currency"`
	State               string           `json:"state"`
	Ambassador          string           `json:"ambassador,omitempty"`
	RoutingReason       string           `json:"routing_reason,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	RefundedAt          *time.Time       `json:"refunded_at,omitempty"`
	RefundedBy          string           `json:"refunded_by,omitempty"`
	CanConfirm          bool             `json:"can_confirm"`
}

type RefundPage struct {
	Items      []RefundTask `json:"items"`
	NextBefore int64        `json:"next_before,omitempty"`
}

type RefundConfirmation struct {
	ID        int64  `json:"id"`
	Version   int64  `json:"version"`
	Key       string `json:"key"`
	Confirmed bool   `json:"confirmed"`
}

const refundColumns = `t.id,t.version,t.displacement_version,t.event_id,t.order_id,t.owner,
 t.payment_attempt,t.amount_cents,t.extras_cents,t.currency,t.state,COALESCE(t.ambassador,''),
 t.routing_reason,t.created_at,t.refunded_at,COALESCE(t.refunded_by,'')`

func scanRefund(row pgx.Row) (RefundTask, error) {
	var task RefundTask
	var raw json.RawMessage
	err := row.Scan(&task.ID, &task.Version, &task.DisplacementVersion, &task.EventID, &task.OrderID, &task.Owner,
		&task.PaymentAttempt, &task.Amount, &raw, &task.Currency, &task.State, &task.Ambassador,
		&task.RoutingReason, &task.CreatedAt, &task.RefundedAt, &task.RefundedBy)
	if err != nil {
		return task, core.DatabaseOperationError(err)
	}
	var cents map[string]int64
	if err = json.Unmarshal(raw, &cents); err != nil {
		return task, err
	}
	task.Extras = make(map[string]Money, len(cents))
	for service, amount := range cents {
		task.Extras[service] = Money(amount)
	}
	return task, nil
}

func createCapacityRefund(ctx context.Context, tx pgx.Tx, order Order, before Choice, removed []string) error {
	if order.State != statePaid {
		return nil
	}
	extras := map[string]int64{}
	var amount Money
	for _, service := range removed {
		price := before.Extras[service]
		if price > 0 {
			extras[service] = int64(price)
			amount += price
		}
	}
	if amount == 0 {
		return nil
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO core.order_refund_tasks
 (order_id,event_id,owner,displacement_version,payment_attempt,amount_cents,extras_cents)
 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(order_id,displacement_version) DO NOTHING RETURNING id`,
		order.ID, order.EventID, order.Owner, order.Version, capacityToken(order), int64(amount), extras).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return auditRefund(ctx, tx, id, order.Owner, "created", 1)
}

func auditRefund(ctx context.Context, tx pgx.Tx, id int64, actor, action string, version int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO core.order_refund_audit(task_id,actor,action,version) VALUES($1,$2,$3,$4)`,
		id, actor, action, version)
	return core.DatabaseOperationError(err)
}

func refundInvalid() error { return problem(http.StatusBadRequest, "refund_invalid") }

func refundReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusNotFound, "refund_not_found")
	}
	return err
}
