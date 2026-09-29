package legacyfood

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

type Notification struct {
	ID         int64           `json:"id"`
	EventID    string          `json:"event_id"`
	Owner      string          `json:"owner"`
	TelegramID int64           `json:"telegram_id"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
}

// QueueReminders uses the same default active pass event and strict deadline
// windows as Python. Imported delivery markers remain distinct from queueing.
func (s Service) QueueReminders(ctx context.Context) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, `WITH chosen AS (
 SELECT f.event_id,f.deadline,f.first_before,f.last_before,f.notify_after FROM core.food_events f JOIN core.pass_events p ON p.id=f.event_id
 WHERE f.bot_id=$1 AND p.finishes_at>clock_timestamp() ORDER BY p.display_order,p.finishes_at,p.id LIMIT 1
), reminder_window AS (
 SELECT f.event_id,f.notify_after,CASE WHEN clock_timestamp()>deadline-first_before
 AND clock_timestamp()<deadline-last_before-notify_after THEN 'first'
 WHEN clock_timestamp()>deadline-last_before AND clock_timestamp()<deadline THEN 'last' END AS phase FROM chosen f
)
INSERT INTO core.food_notifications(event_id,owner,kind,subject,payload)
SELECT f.event_id,o.owner,'reminder_'||f.phase,o.id,jsonb_build_object('order_id',o.id,'phase',f.phase)
FROM reminder_window f JOIN core.food_orders o ON o.event_id=f.event_id
LEFT JOIN LATERAL(SELECT status FROM core.food_payments WHERE order_id=o.id AND kind='meals' ORDER BY generation DESC LIMIT 1)p ON true
WHERE f.phase IS NOT NULL AND o.meal_total>0 AND COALESCE(p.status,'pending') NOT IN ('paid','proof_submitted')
AND o.last_updated<=clock_timestamp()-f.notify_after ON CONFLICT DO NOTHING`, s.BotID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH chosen AS (
 SELECT f.event_id,f.deadline,f.first_before,f.last_before,f.notify_after FROM core.food_events f JOIN core.pass_events p ON p.id=f.event_id
 WHERE f.bot_id=$1 AND p.finishes_at>clock_timestamp() ORDER BY p.display_order,p.finishes_at,p.id LIMIT 1
), reminder_window AS (
 SELECT f.event_id,f.notify_after,CASE WHEN clock_timestamp()>deadline-first_before
 AND clock_timestamp()<deadline-last_before-notify_after THEN 'first'
 WHEN clock_timestamp()>deadline-last_before AND clock_timestamp()<deadline THEN 'last' END AS phase FROM chosen f
)
INSERT INTO core.food_notifications(event_id,owner,kind,payload)
SELECT f.event_id,b.owner,'no_order_'||f.phase,jsonb_build_object('phase',f.phase)
FROM reminder_window f JOIN core.pass_bookings b ON b.event_id=f.event_id AND b.state IN ('assigned','paid')
LEFT JOIN core.food_orders o ON o.event_id=b.event_id AND o.owner=b.owner
LEFT JOIN LATERAL(SELECT status FROM core.food_payments WHERE order_id=o.id AND kind='meals' ORDER BY generation DESC LIMIT 1)p ON true
WHERE f.phase IS NOT NULL AND (o.id IS NULL OR (o.meals='{}'::jsonb AND COALESCE(p.status,'pending') NOT IN ('paid','proof_submitted')))
ON CONFLICT DO NOTHING`, s.BotID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	rows, err := s.DB.Query(ctx, `SELECT n.id,n.event_id,n.owner,u.telegram_id,n.kind,n.payload
FROM core.food_notifications n JOIN core.food_events e ON e.event_id=n.event_id
JOIN core.users u ON u.id=n.owner WHERE e.bot_id=$1 AND n.sent_at IS NULL AND NOT n.imported_sent AND u.can_book
AND (n.kind<>'proof_submitted' OR EXISTS(SELECT 1 FROM core.food_admins a WHERE a.event_id=n.event_id AND a.owner=n.owner AND a.can_review))
AND (n.kind<>'proof_submitted' OR EXISTS(SELECT 1 FROM core.food_payments p WHERE p.order_id=n.payload->>'order_id' AND p.kind=n.payload->>'kind'
 AND p.generation=(n.payload->>'generation')::bigint AND p.status='proof_submitted'
 AND p.generation=(SELECT max(generation) FROM core.food_payments WHERE order_id=p.order_id AND kind=p.kind)))
AND (n.kind NOT LIKE 'reminder_%' AND n.kind NOT LIKE 'no_order_%' OR (
 e.event_id=(SELECT f.event_id FROM core.food_events f JOIN core.pass_events p ON p.id=f.event_id WHERE f.bot_id=$1 AND p.finishes_at>clock_timestamp() ORDER BY p.display_order,p.finishes_at,p.id LIMIT 1)
 AND (right(n.kind,5)='first' AND clock_timestamp()>e.deadline-e.first_before AND clock_timestamp()<e.deadline-e.last_before-e.notify_after
 OR right(n.kind,4)='last' AND clock_timestamp()>e.deadline-e.last_before AND clock_timestamp()<e.deadline)
 AND (n.kind LIKE 'reminder_%' AND EXISTS(SELECT 1 FROM core.food_orders o WHERE o.id=n.subject AND o.meal_total>0 AND o.last_updated<=clock_timestamp()-e.notify_after
 AND COALESCE((SELECT status FROM core.food_payments WHERE order_id=o.id AND kind='meals' ORDER BY generation DESC LIMIT 1),'pending') NOT IN ('paid','proof_submitted'))
 OR n.kind LIKE 'no_order_%' AND EXISTS(SELECT 1 FROM core.pass_bookings b WHERE b.event_id=n.event_id AND b.owner=n.owner AND b.state IN ('assigned','paid'))
 AND NOT EXISTS(SELECT 1 FROM core.food_orders o WHERE o.event_id=n.event_id AND o.owner=n.owner AND (o.meals<>'{}'::jsonb OR COALESCE((SELECT status FROM core.food_payments WHERE order_id=o.id AND kind='meals' ORDER BY generation DESC LIMIT 1),'pending') IN ('paid','proof_submitted'))))))
ORDER BY n.id LIMIT 100`, s.BotID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
		var n Notification
		scanErr := row.Scan(&n.ID, &n.EventID, &n.Owner, &n.TelegramID, &n.Kind, &n.Payload)
		return n, scanErr
	})
}

func (s Service) MarkNotification(ctx context.Context, id int64) error {
	_, err := s.DB.Exec(ctx, `UPDATE core.food_notifications n SET sent_at=clock_timestamp()
FROM core.food_events e WHERE n.event_id=e.event_id AND e.bot_id=$1 AND n.id=$2 AND n.sent_at IS NULL AND NOT n.imported_sent`, s.BotID, id)
	return err
}
