package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/events"
)

// MassageStaffEvents discovers current private grants, independent of passes.
func (s *Store) MassageStaffEvents(
	ctx context.Context,
	scope ProfileScope,
	after uuid.UUID,
	limit int,
) (events.ContextList, error) {
	return s.staffMassageEvents(ctx, scope, nil, after, limit)
}

func (s *Store) MiniAppMassageStaffEvents(
	ctx context.Context,
	deployment EventScope,
	session MiniAppMassageSession,
	after uuid.UUID,
	limit int,
) (events.ContextList, error) {
	return s.staffMassageEvents(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		after,
		limit,
	)
}

func (s *Store) MassageStaffSchedule(
	ctx context.Context,
	scope ProfileScope,
	eventID uuid.UUID,
) (MassageStaffScheduleView, error) {
	return s.staffMassageSchedule(ctx, scope, nil, eventID)
}

func (s *Store) MiniAppMassageStaffSchedule(
	ctx context.Context,
	deployment EventScope,
	session MiniAppMassageSession,
	eventID uuid.UUID,
) (MassageStaffScheduleView, error) {
	return s.staffMassageSchedule(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		eventID,
	)
}

// MassageStaffBookings returns the event-wide timetable or own-only client list.
// Every page rechecks grants. Concurrent changes can require a fresh traversal.
func (s *Store) MassageStaffBookings(
	ctx context.Context,
	scope ProfileScope,
	args MassageStaffQuery,
) (MassageStaffPage, error) {
	return s.staffMassageBookings(ctx, scope, nil, args)
}

func (s *Store) MiniAppMassageStaffBookings(
	ctx context.Context,
	deployment EventScope,
	session MiniAppMassageSession,
	args MassageStaffQuery,
) (MassageStaffPage, error) {
	return s.staffMassageBookings(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		args,
	)
}

// MassageStaffClient requires the booking to belong to a current own alias.
// Cancelled records remain available only through administrative history.
func (s *Store) MassageStaffClient(
	ctx context.Context,
	scope ProfileScope,
	eventID, bookingID uuid.UUID,
) (MassageStaffBooking, error) {
	return s.staffMassageClient(ctx, scope, nil, eventID, bookingID)
}

func (s *Store) MiniAppMassageStaffClient(
	ctx context.Context,
	deployment EventScope,
	session MiniAppMassageSession,
	eventID, bookingID uuid.UUID,
) (MassageStaffBooking, error) {
	return s.staffMassageClient(
		ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization},
		&session,
		eventID,
		bookingID,
	)
}
