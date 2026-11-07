package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// miniAppMassageReadTx holds current owner/session authorization in one snapshot.
// Event reads take no event row lock, so owner -> session -> event reads do not
// reverse the event -> owner lock order used by business writes.
func (s *Store) miniAppMassageReadTx(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID uuid.UUID,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, ProfileScope, error) {
	var event sqlc.ShuttleEvent
	var scope ProfileScope
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, nil, event, scope, err
	}
	attempt, err := s.lockMiniAppBearer(ctx, tx, deployment.BotID, session.Bearer, session.Generation)
	if err == nil && attempt.User.Organization != deployment.Organization {
		err = ErrMiniAppDenied
	}
	q := s.q.WithTx(tx)
	if err == nil {
		scope = ProfileScope{
			BotID:        deployment.BotID,
			Organization: deployment.Organization,
			Subject:      attempt.User.Subject,
			UserID:       attempt.User.ID,
		}
		if eventID != uuid.Nil {
			event, err = q.GetEvent(
				ctx,
				sqlc.GetEventParams{ID: eventID, BotID: deployment.BotID, OrganizationID: deployment.Organization},
			)
		}
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, event, scope, err
	}
	return tx, q, event, scope, nil
}

func (s *Store) OwnMiniAppMassageBooking(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, bookingID uuid.UUID,
) (MassageBooking, error) {
	if eventID == uuid.Nil || bookingID == uuid.Nil {
		return MassageBooking{}, massage.ErrInvalid
	}
	tx, q, _, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, eventID)
	if err != nil {
		return MassageBooking{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.ReadMassageBooking(
		ctx,
		sqlc.ReadMassageBookingParams{ID: bookingID, EventID: eventID, UserID: scope.UserID},
	)
	if err != nil {
		return MassageBooking{}, err
	}
	return massageBookingRecord(row)
}

func (s *Store) OwnMiniAppMassageBookings(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, after uuid.UUID, limit int,
) (MassageBookingPage, error) {
	if eventID == uuid.Nil || limit < 0 || limit > 20 {
		return MassageBookingPage{}, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, q, _, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, eventID)
	if err != nil {
		return MassageBookingPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnMassageBookings(ctx, q, scope, eventID, after, limit)
}

func (s *Store) OwnMiniAppMassageOperation(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, operation uuid.UUID,
) (MassageOperationReceipt, error) {
	if eventID == uuid.Nil || operation == uuid.Nil {
		return MassageOperationReceipt{}, massage.ErrInvalid
	}
	tx, q, _, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, eventID)
	if err != nil {
		return MassageOperationReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, err := readOwnMassageOperation(ctx, q, scope, operation)
	if err == nil && result.EventID != eventID {
		return MassageOperationReceipt{}, pgx.ErrNoRows
	}
	return result, err
}

func (s *Store) OwnMiniAppMassageSchedule(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID uuid.UUID,
) (MassageScheduleView, error) {
	if eventID == uuid.Nil {
		return MassageScheduleView{}, massage.ErrInvalid
	}
	tx, q, _, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, eventID)
	if err != nil {
		return MassageScheduleView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnMassageSchedule(ctx, q, scope, eventID)
}

// OwnMiniAppMassageEvents keeps retained history discoverable after module/pass loss.
func (s *Store) OwnMiniAppMassageEvents(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, after uuid.UUID, limit int,
) (events.ContextList, error) {
	if limit < 0 || limit > events.ContextChoiceLimit {
		return events.ContextList{}, massage.ErrInvalid
	}
	if limit == 0 {
		limit = events.ContextChoiceLimit
	}
	tx, q, _, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, uuid.Nil)
	if err != nil {
		return events.ContextList{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListOwnMassageEvents(ctx, sqlc.ListOwnMassageEventsParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		UserID:         scope.UserID,
		AfterID:        after,
		PageLimit:      int32(limit + 1),
	})
	if err != nil {
		return events.ContextList{}, err
	}
	count := min(len(rows), limit)
	result := events.ContextList{Events: make([]events.ContextEvent, count)}
	for i, row := range rows[:count] {
		result.Events[i] = events.ContextEvent{ID: row.ID, Version: row.Version, Name: row.Name, Timezone: row.Timezone,
			StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC()}
	}
	if len(rows) > count {
		result.Next = &result.Events[count-1].ID
	}
	return result, nil
}
