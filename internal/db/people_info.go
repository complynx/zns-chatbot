package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type PersonCard struct {
	Profile        BasicPerson           `json:"profile"`
	Administration *PersonAdministration `json:"administration,omitempty"`
}
type PersonAdministration struct {
	Scope              string                  `json:"scope"`
	Visible            bool                    `json:"visible"`
	Revoked            bool                    `json:"revoked"`
	Events             []PersonEventIndicators `json:"events"`
	EventsTruncated    bool                    `json:"events_truncated"`
	PastEvents         int64                   `json:"past_events"`
	PastEventSummaries []PersonPastEvent       `json:"past_event_summaries"`
	LatestPastEventAt  *time.Time              `json:"latest_past_event_at,omitempty"`
}
type PersonPastEvent struct {
	EventID   uuid.UUID `json:"event_id"`
	EventName string    `json:"event_name"`
	EndsAt    time.Time `json:"ends_at"`
}
type PersonEventIndicators struct {
	EventID                 uuid.UUID     `json:"event_id"`
	EventName               string        `json:"event_name"`
	EndsAt                  time.Time     `json:"ends_at"`
	Registration            *AccountEvent `json:"registration,omitempty"`
	EventRole               string        `json:"event_role,omitempty"`
	ServiceOrders           *int64        `json:"service_orders,omitempty"`
	MealOrders              *int64        `json:"meal_orders,omitempty"`
	MassageBookings         int64         `json:"massage_bookings"`
	UpcomingMassageBookings int64         `json:"upcoming_massage_bookings"`
}

func (s *Store) PeopleInfo(
	ctx context.Context,
	scope PaymentScope,
	user, event, history uuid.UUID,
) (PersonCard, error) {
	var result PersonCard
	if user == uuid.Nil || history != uuid.Nil && event != uuid.Nil && history != event {
		return result, ErrPeopleInvalid
	}
	// A historical detail read must authorize that exact event, not reuse another event's grant.
	if history != uuid.Nil {
		event = history
	}
	ctx, cancel := context.WithTimeout(ctx, peopleInfoTimeout)
	defer cancel()
	tx, mode, err := s.peopleReadTx(ctx, scope, event)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if scope.GlobalAdmin {
		// Global cards cover every deployment user, including clients without a
		// registration. Event filters below select indicators, not card access.
		mode = peopleScopeAll
	}
	var telegramID int64
	admin := PersonAdministration{Scope: mode, Events: []PersonEventIndicators{}}
	err = tx.QueryRow(ctx, `SELECT `+basicPersonSQL+`,u.visible,u.revoked
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id WHERE `+peopleAccessSQL,
		scope.BotID, scope.Organization, mode, optionalUUID(event), scope.UserID, user).
		Scan(&result.Profile.UserID, &result.Profile.FullName, &result.Profile.Username,
			&telegramID, &result.Profile.Gender, &result.Profile.Pronouns,
			&result.Profile.GenderSource, &result.Profile.PronounsSource,
			&admin.Visible, &admin.Revoked)
	if err != nil {
		return result, err
	}
	result.Profile.FullName = PersonPreferredName(result.Profile.FullName, result.Profile.Username, telegramID)
	result.Profile.ProfileURL = telegram.UserProfileURL(telegramID, result.Profile.Username)
	if mode == peopleScopePublic {
		return result, nil
	}
	result.Administration = &admin
	// Every indicator query has the same deployment/event and specialist-client bounds.
	const eventFilter = `e.bot_id=$1 AND e.organization_id=$2 AND ($3::uuid IS NULL OR e.id=$3)
 AND (($4<>'massage_clients' AND (EXISTS(SELECT 1 FROM shuttle_registrations r WHERE r.event_id=e.id AND r.user_id=$5)
 OR EXISTS(SELECT 1 FROM shuttle_orders o WHERE o.event_id=e.id AND o.user_id=$5)
 OR EXISTS(SELECT 1 FROM shuttle_massage_bookings b WHERE b.event_id=e.id AND b.user_id=$5)))
 OR ($4='massage_clients' AND EXISTS(SELECT 1 FROM shuttle_massage_bookings b
 JOIN shuttle_massage_specialist_bindings sb ON sb.event_id=b.event_id AND sb.specialist_id=b.specialist_id
 WHERE b.event_id=e.id AND b.user_id=$5 AND b.state='booked' AND sb.active AND sb.user_id=$6)))`
	args := []any{scope.BotID, scope.Organization, optionalUUID(event), mode, user, scope.UserID}
	err = tx.QueryRow(ctx, `SELECT count(*),max(e.ends_at) FROM shuttle_events e
 WHERE `+eventFilter+` AND e.ends_at<=statement_timestamp()`, args...).
		Scan(&admin.PastEvents, &admin.LatestPastEventAt)
	if err != nil {
		return result, err
	}
	admin.PastEventSummaries, err = peoplePastSummaries(ctx, tx, eventFilter, args)
	if err != nil {
		return result, err
	}
	err = s.peopleEventDetails(ctx, tx, scope, mode, user, history, eventFilter, args, &admin)
	return result, err
}

func (s *Store) peopleEventDetails(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	mode string,
	user, history uuid.UUID,
	filter string,
	args []any,
	admin *PersonAdministration,
) error {
	query := `SELECT e.id FROM shuttle_events e WHERE ` + filter
	if history == uuid.Nil {
		query += ` AND e.ends_at>statement_timestamp()`
	}
	query += ` ORDER BY e.starts_at,e.id LIMIT 21`
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) > peopleMaxPage {
		admin.EventsTruncated = true
		ids = ids[:peopleMaxPage]
	}
	for _, id := range ids {
		item, readErr := s.peopleEventIndicators(ctx, tx, scope, mode, user, id)
		if readErr != nil {
			return readErr
		}
		admin.Events = append(admin.Events, item)
	}
	return nil
}

func peoplePastSummaries(ctx context.Context, tx pgx.Tx, filter string, args []any) ([]PersonPastEvent, error) {
	result := []PersonPastEvent{}
	rows, err := tx.Query(ctx, `SELECT e.id,e.name,e.ends_at FROM shuttle_events e WHERE `+filter+`
 AND e.ends_at<=statement_timestamp() ORDER BY e.ends_at DESC,e.id LIMIT 5`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item PersonPastEvent
		if err = rows.Scan(&item.EventID, &item.EventName, &item.EndsAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) peopleEventIndicators(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	mode string,
	user, id uuid.UUID,
) (PersonEventIndicators, error) {
	q := s.q.WithTx(tx)
	e, err := q.GetEvent(ctx, sqlc.GetEventParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization})
	if err != nil {
		return PersonEventIndicators{}, err
	}
	item := PersonEventIndicators{EventID: id, EventName: e.Name, EndsAt: e.EndsAt.Time}
	if mode != peopleScopeMassage {
		if err = s.peopleRegistrationIndicators(ctx, tx, e, user, &item); err != nil {
			return item, err
		}
	}
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE b.starts_at>statement_timestamp())
 FROM shuttle_massage_bookings b WHERE b.event_id=$1 AND b.user_id=$2 AND b.state='booked'
 AND ($3<>'massage_clients' OR EXISTS(SELECT 1 FROM shuttle_massage_specialist_bindings sb
 WHERE sb.event_id=b.event_id AND sb.specialist_id=b.specialist_id AND sb.active AND sb.user_id=$4))`,
		id, user, mode, scope.UserID).
		Scan(&item.MassageBookings, &item.UpcomingMassageBookings)
	return item, err
}

func (s *Store) peopleRegistrationIndicators(
	ctx context.Context,
	tx pgx.Tx,
	event sqlc.ShuttleEvent,
	user uuid.UUID,
	item *PersonEventIndicators,
) error {
	q := s.q.WithTx(tx)
	r, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: event.ID, UserID: user})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		entry, readErr := accountEvent(
			ctx,
			q,
			sqlc.ListOwnAccountEventsRow{ShuttleEvent: event, ShuttleRegistration: r},
			time.Now(),
		)
		if readErr != nil {
			return readErr
		}
		item.Registration = &entry
	}
	var services, meals int64
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE order_kind='service'),count(*) FILTER(WHERE order_kind='meal')
 FROM shuttle_orders WHERE event_id=$1 AND user_id=$2 AND state='submitted'`, event.ID, user).Scan(&services, &meals)
	if err != nil {
		return err
	}
	item.ServiceOrders, item.MealOrders = &services, &meals
	return tx.QueryRow(ctx, `SELECT coalesce((SELECT role FROM shuttle_event_authorities
 WHERE event_id=$1 AND user_id=$2),'none')`, event.ID, user).
		Scan(&item.EventRole)
}
