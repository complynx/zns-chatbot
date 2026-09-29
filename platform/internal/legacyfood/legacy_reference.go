package legacyfood

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func noOrder(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func (s Service) resolveSourceOrder(ctx context.Context, actor, objectID string) (Order, error) {
	if err := allowed(ctx, s.DB, actor); err != nil {
		return Order{}, err
	}
	rows, err := s.DB.Query(ctx, `SELECT event_id,target_id FROM core.legacy_food_import_references
 WHERE bot_id=$1 AND source_kind='food' AND lower(source_record->'_id'->>'$oid')=$2 LIMIT 2`, s.BotID, objectID)
	if err != nil {
		return Order{}, err
	}
	defer rows.Close()
	type reference struct{ Event, ID string }
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (reference, error) {
		var ref reference
		scanErr := row.Scan(&ref.Event, &ref.ID)
		return ref, scanErr
	})
	if err != nil {
		return Order{}, err
	}
	if len(refs) != 1 {
		return Order{}, problem("food_order_not_found")
	}
	return s.Get(ctx, actor, refs[0].Event, refs[0].ID)
}
