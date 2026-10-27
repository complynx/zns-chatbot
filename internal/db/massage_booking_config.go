package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// massageCommitmentPageSize matches ReadFutureMassageBookings LIMIT.
const massageCommitmentPageSize = 100

// validateMassageCommitments runs under the shared event lock. It validates
// future/ongoing commitments in bounded pages and checks new physical capacity.
func validateMassageCommitments(
	ctx context.Context,
	q *sqlc.Queries,
	eventID uuid.UUID,
	document massage.ScheduleDocument,
) error {
	after := uuid.Nil
	for {
		rows, err := q.ReadFutureMassageBookings(
			ctx,
			sqlc.ReadFutureMassageBookingsParams{EventID: eventID, AfterID: after},
		)
		if err != nil {
			return err
		}
		for _, row := range rows {
			booking, decodeErr := massageBookingRecord(row)
			if decodeErr != nil {
				return decodeErr
			}
			if !massage.BookingFitsSchedule(document, booking.Snapshot.Terms) {
				return ErrConflict
			}
			after = row.ID
		}
		if len(rows) < massageCommitmentPageSize {
			break
		}
	}
	for _, window := range document.Windows {
		peak, err := q.MassageWindowPeakTableUse(
			ctx,
			sqlc.MassageWindowPeakTableUseParams{
				EventID: eventID,
				FromAt:  massageTimestamp(window.StartsAt),
				UntilAt: massageTimestamp(window.EndsAt),
			},
		)
		if err != nil {
			return err
		}
		if peak > int64(window.Tables) {
			return ErrConflict
		}
	}
	return nil
}
