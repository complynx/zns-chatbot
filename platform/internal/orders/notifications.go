package orders

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const notificationPageSize = 25

type Notification struct {
	ID         int64    `json:"id"`
	Recipient  string   `json:"recipient"`
	TelegramID int64    `json:"telegram_id"`
	EventID    string   `json:"event_id"`
	OrderID    string   `json:"order_id"`
	Kind       string   `json:"kind"`
	Version    int64    `json:"version"`
	Attempt    string   `json:"attempt,omitempty"`
	State      string   `json:"state"`
	Total      Money    `json:"total"`
	Country    string   `json:"country,omitempty"`
	Removed    []string `json:"removed,omitempty"`
	Current    bool     `json:"current"`
}

func enqueueNotification(ctx context.Context, tx pgx.Tx, recipient, kind string, order Order, removed []string) error {
	notice := Notification{
		Recipient: recipient,
		EventID:   order.EventID,
		OrderID:   order.ID,
		Kind:      kind,
		Version:   order.Version,
		Attempt:   order.Attempt,
		State:     order.State,
		Total:     order.Choice.Total,
		Country:   order.Country,
		Removed:   removed,
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.order_notifications(recipient,order_id,payload) VALUES($1,$2,$3)`,
		recipient,
		order.ID,
		notice,
	)
	return err
}

func (op *operation) notifyPayment(ctx context.Context, order Order) error {
	switch op.command.Name {
	case stateCash, actionCountry:
		return enqueueNotification(ctx, op.tx, order.PaymentAdmin, "payment_request", order, nil)
	case actionAccept, actionReject:
		return enqueueNotification(ctx, op.tx, order.Owner, op.command.Name, order, nil)
	}
	return nil
}

// PendingNotifications is a service-only queue. Current marks superseded requests.
func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	rows, err := s.DB.Query(ctx, `SELECT n.id,n.recipient,u.telegram_id,n.payload,
 CASE WHEN n.payload->>'kind'='payment_request' THEN
 o.state IN ('proof','cash') AND o.attempt=n.payload->>'attempt' AND o.payment_admin=n.recipient
	 AND u.can_book AND EXISTS(SELECT 1 FROM core.order_admins a WHERE a.event_id=o.event_id AND a.owner=n.recipient)
	 WHEN n.payload->>'kind' IN ('accept','reject') THEN o.state=n.payload->>'state'
	 WHEN n.payload->>'kind'='reminder' THEN o.state IN ('unpaid','cash') AND (o.choice->>'total')::numeric>0
	 ELSE true END
 FROM core.order_notifications n JOIN core.users u ON u.id=n.recipient JOIN core.orders o ON o.id=n.order_id
	 WHERE n.delivered_at IS NULL AND n.failure='' AND n.available_at<=clock_timestamp()
	 AND (n.payload->>'kind'<>'reminder' OR n.attempted_at IS NULL)
	 AND NOT EXISTS(SELECT 1 FROM core.order_notifications earlier WHERE earlier.recipient=n.recipient
	 AND earlier.id<n.id AND earlier.delivered_at IS NULL AND earlier.failure=''
	 AND (earlier.payload->>'kind'<>'reminder' OR earlier.attempted_at IS NULL))
	 ORDER BY n.available_at,n.id LIMIT $1`, notificationPageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Notification{}
	for rows.Next() {
		var notice Notification
		var id, chat int64
		var recipient string
		var current bool
		if err = rows.Scan(&id, &recipient, &chat, &notice, &current); err != nil {
			return nil, err
		}
		notice.ID, notice.Recipient, notice.TelegramID, notice.Current = id, recipient, chat, current
		result = append(result, notice)
	}
	return result, rows.Err()
}

// CompleteNotification acknowledges successful delivery or a permanent Telegram failure.
func (s Service) CompleteNotification(ctx context.Context, id int64, failure string) error {
	if id <= 0 ||
		(failure != "" && failure != "telegram_forbidden" && failure != "telegram_rejected" && failure != "telegram_retry" && failure != "reminder_failed") {
		return problem(http.StatusBadRequest, "invalid_delivery")
	}
	if failure == "telegram_retry" {
		_, err := s.DB.Exec(
			ctx,
			`UPDATE core.order_notifications SET available_at=clock_timestamp()+interval '5 seconds' WHERE id=$1 AND delivered_at IS NULL`,
			id,
		)
		return err
	}
	result, err := s.DB.Exec(ctx, `UPDATE core.order_notifications SET delivered_at=clock_timestamp(),failure=$2
 WHERE id=$1 AND delivered_at IS NULL`, id, failure)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_notifications WHERE id=$1)`, id).
			Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return problem(http.StatusNotFound, "notification_not_found")
		}
	}
	return nil
}
