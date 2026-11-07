package db

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/massage"
)

type MassageBindingPage struct {
	EventID         uuid.UUID                  `json:"event_id"`
	EventVersion    int64                      `json:"event_version"`
	ScheduleVersion int64                      `json:"schedule_version"`
	ModuleEnabled   bool                       `json:"module_enabled"`
	Bindings        []MassageSpecialistBinding `json:"bindings"`
	NextAfter       string                     `json:"next_after,omitempty"`
}

type MassageBindingTargetQuery struct {
	UserID         uuid.UUID `json:"user_id,omitempty"`
	TelegramUserID int64     `json:"telegram_user_id,omitempty" minimum:"1"`
}

type MassageBindingTarget struct {
	UserID         uuid.UUID `json:"user_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	DisplayName    string    `json:"display_name"`
	Linked         bool      `json:"linked"`
	Revoked        bool      `json:"revoked"`
}

// massageBindingReadTx is for API-authorized global administrators only. It does
// not require a staff grant, pass or enabled module. Event locks precede actor.
func (s *Store) massageBindingReadTx(
	ctx context.Context,
	scope ProfileScope,
	session *MiniAppMassageSession,
	eventID uuid.UUID,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, error) {
	var event sqlc.ShuttleEvent
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, event, err
	}
	q := s.q.WithTx(tx)
	if eventID != uuid.Nil {
		event, err = q.LockContextEvent(
			ctx,
			sqlc.LockContextEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
	if err == nil {
		err = s.lockMassageBindingActor(ctx, tx, scope, session)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, event, err
	}
	return tx, q, event, nil
}

func (s *Store) MassageBindingConfiguration(
	ctx context.Context,
	scope ProfileScope,
	session *MiniAppMassageSession,
	eventID uuid.UUID,
	after string,
	limit int32,
) (MassageBindingPage, error) {
	result := MassageBindingPage{EventID: eventID, Bindings: []MassageSpecialistBinding{}}
	if eventID == uuid.Nil || (after != "" && !massageSpecialistID.MatchString(after)) || limit < 0 || limit > 20 {
		return result, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, q, event, err := s.massageBindingReadTx(ctx, scope, session, eventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result.EventVersion = event.Version
	result.ModuleEnabled = slices.Contains(event.Modules, massageNotificationKind)
	result.ScheduleVersion, err = q.MassageScheduleVersion(ctx, eventID)
	if err != nil {
		return result, err
	}
	rows, err := q.ListMassageSpecialistBindings(
		ctx,
		sqlc.ListMassageSpecialistBindingsParams{EventID: eventID, AfterID: after, PageLimit: limit + 1},
	)
	if err != nil {
		return result, err
	}
	count := min(len(rows), int(limit))
	for _, row := range rows[:count] {
		result.Bindings = append(result.Bindings, massageBindingRecord(row))
	}
	if len(rows) > count {
		result.NextAfter = rows[count-1].SpecialistID
	}
	return result, nil
}

func (s *Store) MassageBinding(
	ctx context.Context,
	scope ProfileScope,
	session *MiniAppMassageSession,
	eventID uuid.UUID,
	specialistID string,
) (MassageSpecialistBinding, error) {
	if eventID == uuid.Nil || !massageSpecialistID.MatchString(specialistID) {
		return MassageSpecialistBinding{}, massage.ErrInvalid
	}
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, session, eventID)
	if err != nil {
		return MassageSpecialistBinding{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.ReadMassageSpecialistBinding(
		ctx,
		sqlc.ReadMassageSpecialistBindingParams{EventID: eventID, SpecialistID: specialistID},
	)
	if err != nil {
		return MassageSpecialistBinding{}, err
	}
	return massageBindingRecord(row), nil
}

// MassageBindingTargetLookup resolves one known UUID OR Telegram user ID in this
// deployment. It is not a directory and never creates an account or exports IAM IDs.
func (s *Store) MassageBindingTargetLookup(
	ctx context.Context,
	scope ProfileScope,
	session *MiniAppMassageSession,
	a MassageBindingTargetQuery,
) (MassageBindingTarget, error) {
	if a.TelegramUserID < 0 || (a.UserID == uuid.Nil) == (a.TelegramUserID == 0) {
		return MassageBindingTarget{}, massage.ErrInvalid
	}
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, session, uuid.Nil)
	if err != nil {
		return MassageBindingTarget{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.ReadMassageBindingTarget(
		ctx,
		sqlc.ReadMassageBindingTargetParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			UserID:         a.UserID,
			TelegramUserID: a.TelegramUserID,
		},
	)
	if err != nil {
		return MassageBindingTarget{}, err
	}
	return MassageBindingTarget{
		UserID:         row.ID,
		TelegramUserID: row.TgUserID,
		DisplayName:    row.DisplayName,
		Linked:         row.Linked,
		Revoked:        row.Revoked,
	}, nil
}

func (s *Store) MassageBindingEvents(
	ctx context.Context,
	scope ProfileScope,
	session *MiniAppMassageSession,
	after uuid.UUID,
	limit int32,
) (events.ContextList, error) {
	result := events.ContextList{Events: []events.ContextEvent{}}
	if limit < 0 || limit > 20 {
		return result, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, session, uuid.Nil)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListMassageBindingEvents(
		ctx,
		sqlc.ListMassageBindingEventsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			AfterID:        after,
			PageLimit:      limit + 1,
		},
	)
	if err != nil {
		return result, err
	}
	count := min(len(rows), int(limit))
	for _, row := range rows[:count] {
		result.Events = append(
			result.Events,
			events.ContextEvent{
				ID:       row.ID,
				Version:  row.Version,
				Name:     row.Name,
				Timezone: row.Timezone,
				StartsAt: row.StartsAt.Time.UTC(),
				EndsAt:   row.EndsAt.Time.UTC(),
			},
		)
	}
	if len(rows) > count {
		result.Next = &result.Events[count-1].ID
	}
	return result, nil
}

func (s *Store) MassageBindingSchedule(
	ctx context.Context,
	scope ProfileScope,
	session *MiniAppMassageSession,
	eventID uuid.UUID,
) (MassageScheduleView, error) {
	if eventID == uuid.Nil {
		return MassageScheduleView{}, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBindingReadTx(ctx, scope, session, eventID)
	if err != nil {
		return MassageScheduleView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readMassageScheduleView(ctx, q, event)
}
