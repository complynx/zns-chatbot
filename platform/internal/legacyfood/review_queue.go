package legacyfood

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// ReviewQueue lists only orders with a currently submitted payment. The cursor
// carries navigation, never authority; each page checks the current grant.
func (s Service) ReviewQueue(ctx context.Context, actor, event, raw string) (core.ReadPage[ReviewItem], error) {
	result := core.ReadPage[ReviewItem]{}
	if _, err := s.Event(ctx, actor, event); err != nil {
		return result, err
	}
	if err := adminAllowed(ctx, s.DB, actor, event, "review"); err != nil {
		return result, err
	}
	cursor, err := core.DecodeReadCursor(raw, actor, "food.review.queue:"+event)
	if err != nil {
		return result, err
	}
	rows, err := s.DB.Query(ctx, `SELECT o.id,o.owner,o.version FROM core.food_orders o
 WHERE o.event_id=$1 AND o.id>$2 AND EXISTS(SELECT 1 FROM core.food_payments p
 WHERE p.order_id=o.id AND p.status='proof_submitted' AND p.generation=(SELECT max(n.generation)
 FROM core.food_payments n WHERE n.order_id=p.order_id AND n.kind=p.kind))
 ORDER BY o.id LIMIT $3`, event, cursor.Position, core.ReadPageItems+1)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	items := []ReviewItem{}
	for rows.Next() {
		var item ReviewItem
		if err = rows.Scan(&item.OrderID, &item.Owner, &item.Version); err != nil {
			return result, core.DatabaseOperationError(err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return result, core.DatabaseOperationError(err)
	}
	return core.NavigationPage(items, cursor, func(item ReviewItem) string { return item.OrderID })
}

type ReviewItem struct {
	OrderID string `json:"order_id"`
	Owner   string `json:"owner"`
	Version int64  `json:"version"`
}
