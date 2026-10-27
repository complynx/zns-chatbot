package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// MassageStaffCursor belongs to one event, window, range and ownership filter.
// It is a continuation position, never an access grant or a snapshot lease.
type MassageStaffCursor struct {
	EventID  uuid.UUID `json:"event_id"`
	WindowID string    `json:"window_id"`
	From     time.Time `json:"from"`
	Until    time.Time `json:"until"`
	OwnOnly  bool      `json:"own_only"`
	StartsAt time.Time `json:"starts_at"`
	ID       uuid.UUID `json:"id"`
}

type MassageStaffQuery struct {
	EventID  uuid.UUID           `json:"event_id"`
	WindowID string              `json:"window_id"`
	From     time.Time           `json:"from"`
	Until    time.Time           `json:"until"`
	OwnOnly  bool                `json:"own_only,omitempty"`
	After    *MassageStaffCursor `json:"after,omitempty"`
	Limit    int32               `json:"limit,omitempty"`
}

// MassageStaffBooking exposes frozen terms and a bounded client display name.
// Contact is present only when the current actor owns the specialist alias.
type MassageStaffBooking struct {
	ID         uuid.UUID            `json:"id"`
	Version    int64                `json:"version"`
	Terms      massage.BookingTerms `json:"terms"`
	ClientName string               `json:"client_name"`
	Contact    string               `json:"contact,omitempty"`
}

type MassageStaffPage struct {
	EventID          uuid.UUID             `json:"event_id"`
	EventVersion     int64                 `json:"event_version"`
	ScheduleVersion  int64                 `json:"schedule_version"`
	Timezone         string                `json:"timezone"`
	ObservedAt       time.Time             `json:"observed_at"`
	OwnSpecialistIDs []string              `json:"own_specialist_ids"`
	Bookings         []MassageStaffBooking `json:"bookings"`
	Next             *MassageStaffCursor   `json:"next,omitempty"`
}

type MassageStaffScheduleView struct {
	MassageScheduleView

	OwnSpecialistIDs []string `json:"own_specialist_ids"`
}

func normalizeMassageStaffQuery(a MassageStaffQuery) (MassageStaffQuery, error) {
	if a.EventID == uuid.Nil || !massageSpecialistID.MatchString(a.WindowID) ||
		a.From.IsZero() || a.Until.IsZero() || a.From.Year() < 1 || a.Until.Year() > 9999 ||
		!a.From.Before(a.Until) || a.Until.Sub(a.From) > 24*time.Hour || a.Limit < 0 || a.Limit > 20 {
		return a, massage.ErrInvalid
	}
	a.From, a.Until = a.From.UTC(), a.Until.UTC()
	if a.Limit == 0 {
		a.Limit = 20
	}
	if c := a.After; c != nil && (c.EventID != a.EventID || c.WindowID != a.WindowID ||
		!c.From.Equal(a.From) || !c.Until.Equal(a.Until) || c.OwnOnly != a.OwnOnly ||
		c.ID == uuid.Nil || c.StartsAt.IsZero() || !c.StartsAt.Before(a.Until)) {
		return a, massage.ErrInvalid
	}
	return a, nil
}

type massageStaffRead struct {
	tx     pgx.Tx
	q      *sqlc.Queries
	event  sqlc.ShuttleEvent
	ownIDs []string
}

// Staff IAM validation belongs at the HTTP/API boundary. These reads additionally
// recheck local identity, current event grants and, for native callers, session.
// READ COMMITTED is deliberate: a grant revoked while waiting for the event lock
// must not survive in an earlier repeatable-read snapshot. Event SHARE fences
// binding, schedule and booking writes for the remainder of this read.
func (s *Store) massageStaffReadTx(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, eventID uuid.UUID,
) (massageStaffRead, error) {
	var r massageStaffRead
	if eventID == uuid.Nil {
		return r, massage.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return r, err
	}
	r.tx, r.q = tx, s.q.WithTx(tx)
	r.event, err = r.q.LockContextEvent(ctx, sqlc.LockContextEventParams{
		ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err == nil {
		scope, err = s.lockMassageStaffActor(ctx, tx, scope, session)
	}
	if err == nil {
		r.ownIDs, err = r.q.OwnActiveMassageSpecialistIDs(
			ctx,
			sqlc.OwnActiveMassageSpecialistIDsParams{EventID: eventID, UserID: scope.UserID},
		)
	}
	if err == nil && len(r.ownIDs) == 0 {
		err = pgx.ErrNoRows
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
	}
	return r, err
}

func (s *Store) lockMassageStaffActor(ctx context.Context, tx pgx.Tx, scope ProfileScope,
	session *MiniAppMassageSession,
) (ProfileScope, error) {
	if session == nil {
		return scope, lockProfileOwner(ctx, tx, scope, false)
	}
	attempt, err := s.lockMiniAppBearer(ctx, tx, scope.BotID, session.Bearer, session.Generation)
	if err != nil {
		return ProfileScope{}, err
	}
	if attempt.User.Organization != scope.Organization {
		return ProfileScope{}, ErrMiniAppDenied
	}
	return ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		Subject:      attempt.User.Subject,
		UserID:       attempt.User.ID,
	}, nil
}

func (s *Store) staffMassageEvents(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, after uuid.UUID, limit int,
) (events.ContextList, error) {
	result := events.ContextList{Events: []events.ContextEvent{}}
	if limit < 0 || limit > events.ContextChoiceLimit {
		return result, massage.ErrInvalid
	}
	if limit == 0 {
		limit = events.ContextChoiceLimit
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	scope, err = s.lockMassageStaffActor(ctx, tx, scope, session)
	if err != nil {
		return result, err
	}
	// Discovery takes no event locks. One statement observes current grants; every
	// subsequent schedule/page/detail request authorizes again under its event lock.
	rows, err := s.q.WithTx(tx).ListMassageStaffEvents(ctx, sqlc.ListMassageStaffEventsParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		UserID:         scope.UserID,
		AfterID:        after,
		PageLimit:      int32(limit + 1),
	})
	if err != nil {
		return result, err
	}
	count := min(len(rows), limit)
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

func (s *Store) staffMassageSchedule(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, eventID uuid.UUID,
) (MassageStaffScheduleView, error) {
	var result MassageStaffScheduleView
	r, err := s.massageStaffReadTx(ctx, scope, session, eventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, r.tx)
	result.MassageScheduleView, err = readMassageScheduleView(ctx, r.q, r.event)
	result.OwnSpecialistIDs = r.ownIDs
	return result, err
}

func massageStaffBookingRecord(row sqlc.ListMassageStaffBookingsRow) (MassageStaffBooking, error) {
	booking, err := massageBookingRecord(row.ShuttleMassageBooking)
	if err != nil {
		return MassageStaffBooking{}, err
	}
	result := MassageStaffBooking{
		ID:         booking.ID,
		Version:    booking.Version,
		Terms:      booking.Snapshot.Terms,
		ClientName: row.ClientName,
	}
	if row.ContactID > 0 {
		result.Contact = fmt.Sprintf("tg://user?id=%d", row.ContactID)
	}
	return result, nil
}

func (s *Store) staffMassageBookings(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, args MassageStaffQuery,
) (MassageStaffPage, error) {
	result := MassageStaffPage{Bookings: []MassageStaffBooking{}}
	args, err := normalizeMassageStaffQuery(args)
	if err != nil {
		return result, err
	}
	r, err := s.massageStaffReadTx(ctx, scope, session, args.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, r.tx)
	result.EventID, result.EventVersion, result.Timezone = r.event.ID, r.event.Version, r.event.Timezone
	result.OwnSpecialistIDs = r.ownIDs
	result.ScheduleVersion, err = r.q.MassageScheduleVersion(ctx, r.event.ID)
	if err != nil {
		return result, err
	}
	now, err := r.q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	result.ObservedAt = now.Time.UTC()
	params := sqlc.ListMassageStaffBookingsParams{
		EventID:  r.event.ID,
		OwnIds:   r.ownIDs,
		OwnOnly:  args.OwnOnly,
		WindowID: args.WindowID,
		FromAt:   pgtype.Timestamptz{Time: args.From, Valid: true},
		UntilAt: pgtype.Timestamptz{
			Time:  args.Until,
			Valid: true,
		},
		AfterAt:   pgtype.Timestamptz{Time: args.From, Valid: true},
		PageLimit: args.Limit + 1,
	}
	if args.After != nil {
		params.AfterID, params.AfterAt.Time = args.After.ID, args.After.StartsAt
	}
	rows, err := r.q.ListMassageStaffBookings(ctx, params)
	if err != nil {
		return result, err
	}
	for i, row := range rows {
		if i == int(args.Limit) {
			break
		}
		record, decodeErr := massageStaffBookingRecord(row)
		if decodeErr != nil {
			return result, decodeErr
		}
		result.Bookings = append(result.Bookings, record)
		result.Next = staffMassageCursor(args, row.ShuttleMassageBooking)
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			return result, encodeErr
		}
		// Leave room for the API envelope and bound a page below Loom's 64 KiB limit.
		if len(encoded) > 56*1024 {
			result.Bookings = result.Bookings[:len(result.Bookings)-1]
			break
		}
	}
	count := len(result.Bookings)
	if count > 0 && count < len(rows) {
		result.Next = staffMassageCursor(args, rows[count-1].ShuttleMassageBooking)
	} else {
		result.Next = nil
	}
	return result, nil
}

func staffMassageCursor(args MassageStaffQuery, row sqlc.ShuttleMassageBooking) *MassageStaffCursor {
	return &MassageStaffCursor{
		EventID:  args.EventID,
		WindowID: args.WindowID,
		From:     args.From,
		Until:    args.Until,
		OwnOnly:  args.OwnOnly,
		StartsAt: row.StartsAt.Time.UTC(),
		ID:       row.ID,
	}
}

func (s *Store) staffMassageClient(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, eventID, bookingID uuid.UUID,
) (MassageStaffBooking, error) {
	if bookingID == uuid.Nil {
		return MassageStaffBooking{}, massage.ErrInvalid
	}
	r, err := s.massageStaffReadTx(ctx, scope, session, eventID)
	if err != nil {
		return MassageStaffBooking{}, err
	}
	defer rollbackLegacyArchive(ctx, r.tx)
	rows, err := r.q.ListMassageStaffBookings(ctx, sqlc.ListMassageStaffBookingsParams{
		EventID: eventID, BookingID: bookingID, OwnIds: r.ownIDs, OwnOnly: true,
		FromAt:    pgtype.Timestamptz{InfinityModifier: pgtype.NegativeInfinity, Valid: true},
		UntilAt:   pgtype.Timestamptz{InfinityModifier: pgtype.Infinity, Valid: true},
		PageLimit: 1,
	})
	if err != nil {
		return MassageStaffBooking{}, err
	}
	if len(rows) == 0 {
		return MassageStaffBooking{}, pgx.ErrNoRows
	}
	return massageStaffBookingRecord(rows[0])
}
