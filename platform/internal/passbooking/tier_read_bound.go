package passbooking

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

// Every encoded Tier takes at least 80 bytes, even with minimum field values.
// This coarse preflight bounds allocation; the complete result is checked below.
const minimumTierBytes = 80

func readTierEvent(ctx context.Context, tx pgx.Tx, actor, eventID string) (event, error) {
	e := event{id: eventID, tiers: []passallocation.Tier{}}
	err := tx.QueryRow(ctx, `SELECT assignment_rule FROM core.pass_events WHERE id=$1 FOR NO KEY UPDATE`, eventID).
		Scan(&e.rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return event{}, conflict("pass_event_unknown")
	}
	if err != nil {
		return event{}, core.DatabaseOperationError(err)
	}
	if _, err = authorize(ctx, tx, actor, commandAdminCancel, eventID); err != nil {
		return event{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM core.pass_event_tiers WHERE event_id=$1`, eventID).
		Scan(&count); err != nil {
		return event{}, core.DatabaseOperationError(err)
	}
	if count > core.ReadResourceBytes/minimumTierBytes {
		return event{}, core.ReadProblem("read_result_limit")
	}
	rows, err := tx.Query(
		ctx,
		`SELECT position,amount,price,starts_at,promo,blocked_by_date FROM core.pass_event_tiers WHERE event_id=$1 ORDER BY position`,
		eventID,
	)
	if err != nil {
		return event{}, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var position int
		var tier passallocation.Tier
		if err = rows.Scan(
			&position,
			&tier.Amount,
			&tier.Price,
			&tier.Start,
			&tier.Promo,
			&tier.BlockedByDate,
		); err != nil {
			return event{}, core.DatabaseOperationError(err)
		}
		if position != len(e.tiers) {
			return event{}, conflict("pass_tiers_invalid")
		}
		e.tiers = append(e.tiers, tier)
	}
	return e, core.DatabaseOperationError(rows.Err())
}
func checkTierResult(result TierStatus) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	// API handlers encode one trailing newline after the complete value.
	if len(data)+1 > core.ReadResourceBytes {
		return core.ReadProblem("read_result_limit")
	}
	return nil
}
