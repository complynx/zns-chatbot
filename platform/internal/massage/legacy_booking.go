package massage

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Imported booking callbacks resolve an owner-scoped immutable reference, then
// use Execute so retries, current ownership, versions and notices stay shared.
func (s Service) executeLegacyBooking(ctx context.Context, actor string, c LegacyCommand) (LegacyDraft, error) {
	var result LegacyDraft
	err := s.DB.QueryRow(ctx, `SELECT b.id,b.event_id,b.version,b.cancelled_at IS NOT NULL FROM core.legacy_massage_import_references r
 JOIN core.massage_bookings b ON b.id=r.target_id
 WHERE r.source_kind='booking' AND r.owner=$1 AND r.event_id=$2 AND b.owner=$1
 AND (r.target_id=$3 OR r.source_record->'_id'->>'$oid'=$3)`, actor, c.Event, c.ID).
		Scan(&result.Booking, &result.Event, &result.Version, &result.Cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusNotFound, "not_found")
	}
	if err != nil {
		return result, err
	}
	result.ID = result.Booking
	result.Closed = true
	if c.Action == legacySourceAction && c.Choice == 1 || c.Action == legacySourceCancel {
		booking, executeErr := s.Execute(
			ctx,
			actor,
			Command{
				Key:     "legacy-cancel:" + c.Key,
				Action:  legacyCancel,
				Event:   c.Event,
				Booking: result.Booking,
				Version: 1,
			},
		)
		if executeErr != nil {
			return result, executeErr
		}
		result.Version = booking.Version
		result.Cancelled = booking.CancelledAt != nil
		return result, nil
	}
	if c.Action != legacySourceAction && c.Action != legacySourceBack {
		return result, problem(http.StatusConflict, "stale_version")
	}
	return result, nil
}
