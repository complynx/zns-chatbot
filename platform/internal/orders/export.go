package orders

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const MaxExportBytes = 20 << 20
const maxExportChoiceBytes = 16 << 20
const maxExportOrders = 10000

type exportOrder struct {
	Order        Order
	TelegramID   int64
	PaymentAdmin string
	UpdatedAt    time.Time
}

// Export produces one event snapshot under the requesting administrator's rights.
func (s Service) Export(ctx context.Context, actor, event string) ([]byte, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or a reported error.
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.order_admins a JOIN core.users u ON u.id=a.owner
	 WHERE a.event_id=$1 AND a.owner=$2 AND u.can_book)`, event, actor).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, problem(http.StatusForbidden, "forbidden")
	}
	var count, size int64
	err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(octet_length(choice::text)),0)
	 FROM core.orders WHERE event_id=$1 AND state<>'deleted'`, event).Scan(&count, &size)
	if err != nil {
		return nil, err
	}
	if count > maxExportOrders || size > maxExportChoiceBytes {
		return nil, problem(http.StatusRequestEntityTooLarge, "export_too_large")
	}
	var raw json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT menu FROM core.order_events WHERE id=$1`, event).Scan(&raw); err != nil {
		return nil, err
	}
	var catalog Catalog
	if err = json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	list, err := exportSnapshot(ctx, tx, event)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return renderExport(ctx, catalog, list)
}

func exportSnapshot(ctx context.Context, tx pgx.Tx, event string) ([]exportOrder, error) {
	rows, err := tx.Query(ctx, `SELECT to_jsonb(o),u.telegram_id,COALESCE(a.telegram_id::text,''),o.updated_at
	 FROM core.orders o JOIN core.users u ON u.id=o.owner LEFT JOIN core.users a ON a.id=o.payment_admin
	 WHERE o.event_id=$1 AND o.state<>'deleted' ORDER BY o.created_at,o.id`, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []exportOrder{}
	for rows.Next() {
		var entry exportOrder
		if err = rows.Scan(&entry.Order, &entry.TelegramID, &entry.PaymentAdmin, &entry.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, entry)
	}
	return list, rows.Err()
}
