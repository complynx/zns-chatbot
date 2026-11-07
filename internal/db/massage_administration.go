package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

type MassageAdministrationBooking struct {
	UserID  uuid.UUID      `json:"user_id"`
	Booking MassageBooking `json:"booking"`
}

type MassageAdministrationPage struct {
	EventID    uuid.UUID                      `json:"event_id"`
	Timezone   string                         `json:"timezone"`
	ObservedAt time.Time                      `json:"observed_at"`
	Bookings   []MassageAdministrationBooking `json:"bookings"`
	NextAfter  *uuid.UUID                     `json:"next_after,omitempty"`
}

// AdministrativeMassageBookings requires global administrator authorization at
// the API boundary. Retained event history needs no pass or enabled module.
func (s *Store) AdministrativeMassageBookings(ctx context.Context, scope ProfileScope,
	eventID, after uuid.UUID, limit int,
) (MassageAdministrationPage, error) {
	result := MassageAdministrationPage{EventID: eventID, Bookings: []MassageAdministrationBooking{}}
	if eventID == uuid.Nil || limit < 0 || limit > 20 {
		return result, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return result, err
	}
	q := s.q.WithTx(tx)
	event, err := q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return result, err
	}
	result.Timezone = event.Timezone
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	result.ObservedAt = now.Time.UTC()
	rows, err := q.ListEventMassageBookings(
		ctx,
		sqlc.ListEventMassageBookingsParams{EventID: eventID, ID: after, Limit: int32(limit + 1)},
	)
	if err != nil {
		return result, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[len(rows)-1].ID
		result.NextAfter = &next
	}
	for _, row := range rows {
		booking, decodeErr := massageBookingRecord(row)
		if decodeErr != nil {
			return result, decodeErr
		}
		result.Bookings = append(result.Bookings, MassageAdministrationBooking{UserID: row.UserID, Booking: booking})
	}
	return result, nil
}
