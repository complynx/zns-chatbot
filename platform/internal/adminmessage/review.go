package adminmessage

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Page struct {
	ID     int64      `json:"id"`
	State  string     `json:"state"`
	Total  int64      `json:"total"`
	Offset int64      `json:"offset"`
	More   bool       `json:"more"`
	Items  []Delivery `json:"items"`
}

// Review pages the complete immutable audience, including unsent draft content.
func (s Service) Review(ctx context.Context, actor string, id, offset int64) (Page, error) {
	result := Page{ID: id, Offset: offset}
	if offset < 0 {
		return result, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := owned(ctx, tx, actor, id)
	if err != nil {
		return result, err
	}
	result.State = message.State
	result.Total = int64(len(message.Request.Destinations))
	const pageSize int64 = 20
	rows, err := tx.Query(
		ctx,
		`SELECT COALESCE(d.id,0),m.id,r.destination,r.content,COALESCE(d.state,m.state),COALESCE(d.attempt,0),COALESCE(d.telegram_message_id,0),CASE WHEN r.failure<>'' THEN r.failure ELSE COALESCE(d.failure,'') END
 FROM core.admin_messages m JOIN core.admin_message_recipients r ON r.message_id=m.id
 LEFT JOIN core.admin_message_deliveries d ON d.message_id=m.id AND d.destination=r.destination
 WHERE m.id=$1 AND r.position>$2 ORDER BY r.position LIMIT $3`,
		id,
		offset,
		pageSize,
	)
	if err != nil {
		return result, err
	}
	result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
		var item Delivery
		scanErr := row.Scan(
			&item.ID,
			&item.MessageID,
			&item.Destination,
			&item.Content,
			&item.State,
			&item.Attempt,
			&item.TelegramMessageID,
			&item.Failure,
		)
		return item, scanErr
	})
	if err != nil {
		return result, err
	}
	result.More = offset+int64(len(result.Items)) < result.Total
	return result, tx.Commit(ctx)
}
