package migrate

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/importdelivery"
)

// Capture the initial notice set before normal runtime reminder creation. Only
// additional notices are new delivery obligations; source sent flags stay unbound.
func registerMassageDelivery(ctx context.Context, tx pgx.Tx, p preparedMassage) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT n.id,n.kind,n.delivery_chat FROM core.massage_notices n
JOIN core.massage_bookings b ON b.id=n.booking_id WHERE b.event_id=ANY($1::text[]) ORDER BY n.id`, p.eventNames())
	if err != nil {
		return nil, errors.New("massage_delivery_unavailable")
	}
	defer rows.Close()
	ids := []int64{}
	var pending []importdelivery.MassageNotice
	for rows.Next() {
		var id, chat int64
		var kind string
		if err = rows.Scan(&id, &kind, &chat); err != nil {
			return nil, errors.New("massage_delivery_unavailable")
		}
		ids = append(ids, id)
		if kind == "additional" {
			pending = append(pending, importdelivery.MassageNotice{ID: id, Chat: chat})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, errors.New("massage_delivery_unavailable")
	}
	rows.Close()
	if err = importdelivery.RegisterMassage(ctx, tx, p.Plan.BotID, pending); err != nil {
		return nil, errors.New("massage_delivery_conflict")
	}
	return ids, nil
}
