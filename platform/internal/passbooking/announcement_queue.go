package passbooking

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

const announcementCandidateLimit = 100

func announcementReference(id int64) delivery.Reference {
	return delivery.Reference{Owner: delivery.Announcement, Key: strconv.FormatInt(id, 10), Effect: "send"}
}

func enqueueRegistrationAnnouncements(
	ctx context.Context,
	tx pgx.Tx,
	eventID string,
	botID int64,
	bindings *destination.Bindings,
	observed *time.Time,
) ([]delivery.Registration, error) {
	rows, err := tx.Query(
		ctx,
		`INSERT INTO core.pass_registration_announcements(event_id,owner,created_at,channel,thread_id,locale,name,role,state,bot_id)
 SELECT b.event_id,b.owner,b.created_at,e.thread_channel,e.thread_id,e.thread_locale,u.name,b.role,
 CASE WHEN e.thread_channel='' THEN 'suppressed' ELSE 'pending' END,CASE WHEN $2::bigint>0 THEN $2::bigint END
 FROM core.pass_bookings b JOIN core.pass_events e ON e.id=b.event_id JOIN core.users u ON u.id=b.owner
 LEFT JOIN LATERAL (SELECT candidate.id,candidate.origin,candidate.state,candidate.effective_position FROM core.registration_intents candidate
 WHERE candidate.event_id=b.event_id AND candidate.owner IN (b.owner,NULLIF(b.partner,'')) AND candidate.state<>'cancelled'
 ORDER BY (candidate.owner=b.owner) DESC LIMIT 1) i ON true
 WHERE b.event_id=$1 AND b.state<>'cancelled' AND (e.open_ended OR e.finishes_at>COALESCE($3::timestamptz,clock_timestamp()))
 AND (i.id IS NULL OR i.origin='legacy_fallback' OR (i.state='registered' AND NOT EXISTS(
 SELECT 1 FROM core.registration_intents head WHERE head.event_id=b.event_id AND head.state='captured'
 AND head.origin='canonical_ingress' AND head.effective_position<i.effective_position)
 AND NOT EXISTS(SELECT 1 FROM core.registration_ingress g
 LEFT JOIN core.registration_intents active ON active.event_id=g.native_event AND active.owner=g.native_owner AND active.state<>'cancelled'
 WHERE g.native_event=b.event_id AND g.native_payload IS NOT NULL AND g.native_outcome=''
 AND (active.id IS NULL OR active.state='captured')
 AND COALESCE(active.effective_position,g.id)<i.effective_position
 AND NOT EXISTS(SELECT 1 FROM core.registration_intents retired WHERE retired.event_id=g.native_event AND retired.owner=g.native_owner AND retired.state='cancelled' AND retired.closed_through_position>=g.id))))
 ORDER BY CASE WHEN i.origin='canonical_ingress' THEN 1 ELSE 0 END,i.effective_position,b.created_at,b.owner
 ON CONFLICT(event_id,owner,created_at) DO NOTHING RETURNING id,channel,thread_id,state`,
		eventID,
		botID,
		observed,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	type queued struct {
		id      int64
		channel string
		chat    string
		thread  pgtype.Int8
		state   string
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (queued, error) {
		var item queued
		scanErr := row.Scan(&item.id, &item.channel, &item.thread, &item.state)
		return item, scanErr
	})
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	if botID <= 0 {
		return []delivery.Registration{}, nil
	}
	resolved := make(map[string]string)
	for index := range items {
		item := &items[index]
		if item.state != operationPending {
			continue
		}
		chat, exists := resolved[item.channel]
		if !exists {
			var lookupErr error
			chat, lookupErr = bindings.Lookup(item.channel)
			if lookupErr != nil {
				return nil, &core.ProblemError{Status: http.StatusServiceUnavailable, Code: "destination_unavailable"}
			}
			resolved[item.channel] = chat
		}
		item.chat = chat
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	registrations := make([]delivery.Registration, 0, len(items))
	for _, item := range items {
		if item.state != operationPending {
			continue
		}
		registrations = append(
			registrations,
			delivery.Registration{
				Reference:   announcementReference(item.id),
				Destination: delivery.Destination{Chat: item.chat, Thread: item.thread.Int64},
				Class:       delivery.Background,
			},
		)
	}
	return registrations, nil
}

// RefreshAnnouncementDestinations performs provider I/O without an owner transaction.
// Runtime composition refreshes this snapshot before accepting alias publications.
func (s Service) RefreshAnnouncementDestinations(
	ctx context.Context,
	resolver destination.Resolver,
	ttl time.Duration,
) error {
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return err
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT DISTINCT thread_channel FROM core.pass_events WHERE thread_channel<>'' AND (open_ended OR finishes_at>COALESCE($1::timestamptz,clock_timestamp())) ORDER BY thread_channel`,
		observed,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	chats, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return s.AnnouncementBindings.Refresh(ctx, resolver, chats, ttl)
}

func (s Service) PrepareRegistrationAnnouncement(
	ctx context.Context,
	id int64,
) (RegistrationAnnouncement, bool, error) {
	if err := s.Delivery.Validate(); err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return RegistrationAnnouncement{}, false, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	if err = s.lockAnnouncementSource(ctx, tx, id); errors.Is(err, pgx.ErrNoRows) {
		return RegistrationAnnouncement{}, false, nil
	} else if err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	var item RegistrationAnnouncement
	var thread pgtype.Int8
	err = tx.QueryRow(ctx, `SELECT id,channel,thread_id,locale,name,role,attempts+1 FROM core.pass_registration_announcements
 WHERE id=$1 AND bot_id=$2 AND state='pending' AND available_at<=clock_timestamp()
 AND (lease_until IS NULL OR lease_until<=clock_timestamp()) FOR UPDATE SKIP LOCKED`, id, s.Delivery.BotID).
		Scan(&item.ID, &item.Channel, &thread, &item.Locale, &item.Name, &item.Role, &item.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, core.DatabaseOperationError(err)
	}
	entry, err := delivery.ReadReference(ctx, tx, s.Delivery.BotID, announcementReference(id))
	if err != nil {
		return item, false, err
	}
	if entry.Destination.Thread != thread.Int64 {
		return item, false, delivery.ErrQueueBinding
	}
	item.Channel = entry.Destination.Chat
	if thread.Valid {
		item.ThreadID = &thread.Int64
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.pass_registration_announcements SET attempts=$2,lease_until=clock_timestamp()+interval '2 minutes' WHERE id=$1`,
		id,
		item.Attempts,
	)
	if err != nil {
		return item, false, core.DatabaseOperationError(err)
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	return item, true, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) lockAnnouncementSource(ctx context.Context, tx pgx.Tx, id int64) error {
	var eventID, owner string
	if err := tx.QueryRow(ctx, `SELECT event_id,owner FROM core.pass_registration_announcements WHERE id=$1 AND bot_id=$2`, id, s.Delivery.BotID).
		Scan(&eventID, &owner); err != nil {
		return core.DatabaseOperationError(err)
	}
	q := dbgen.New(tx)
	if _, err := q.LockAnnouncementEvent(ctx, eventID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationError(err)
	}
	if _, err := q.LockAnnouncementBooking(
		ctx,
		dbgen.LockAnnouncementBookingParams{EventID: eventID, Owner: owner},
	); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationError(err)
	}
	return nil
}

// RecoverRegistrationAnnouncements retires invalid unsent effects and preserves
// uncertain wire attempts. Each owner transaction acquires its own lane only.
func (s Service) RecoverRegistrationAnnouncements(ctx context.Context) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return err
	}
	rows, err := s.DB.Query(ctx, `SELECT a.id FROM core.pass_registration_announcements a WHERE a.bot_id=$1 AND
 ((a.state='sending' AND a.lease_until<=clock_timestamp()) OR
 (a.state='pending' AND NOT EXISTS(SELECT 1 FROM core.pass_events e JOIN core.pass_bookings b ON b.event_id=e.id
 WHERE e.id=a.event_id AND b.owner=a.owner AND b.created_at=a.created_at AND b.state<>'cancelled'
 AND (e.open_ended OR e.finishes_at>COALESCE($2::timestamptz,clock_timestamp())) AND e.thread_channel=a.channel
 AND COALESCE(e.thread_id,0)=COALESCE(a.thread_id,0)))) ORDER BY a.id LIMIT 100`, s.Delivery.BotID, observed)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	for _, id := range ids {
		if err = s.recoverAnnouncement(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
func (s Service) recoverAnnouncement(ctx context.Context, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	if err = s.lockAnnouncementSource(ctx, tx, id); err != nil {
		return err
	}
	var attempt int64
	if err = tx.QueryRow(ctx, `SELECT attempts FROM core.pass_registration_announcements WHERE id=$1 AND bot_id=$2 FOR UPDATE`, id, s.Delivery.BotID).
		Scan(&attempt); err != nil {
		return core.DatabaseOperationError(err)
	}
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return err
	}
	q := dbgen.New(tx)
	row, err := q.LockAnnouncementAttempt(
		ctx,
		dbgen.LockAnnouncementAttemptParams{
			ID:         id,
			BotID:      s.Delivery.BotID,
			Attempt:    attempt,
			DomainTime: nullableRegistrationTime(observed),
		},
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	var state delivery.Kind
	reason := ""
	if row.State == "sending" && !row.LeaseLive {
		state = delivery.Uncertain
		reason = "telegram_outcome_unknown"
	}
	if row.State == operationPending && !row.Current {
		state = delivery.Cancelled
		reason = "announcement_superseded"
	}
	if reason != "" {
		if err = delivery.Project(
			ctx,
			tx,
			s.Delivery.BotID,
			announcementReference(id),
			state,
			time.Time{},
		); err != nil {
			return err
		}
		if err = s.finishAnnouncement(
			ctx,
			q,
			delivery.Attempt{ID: id, Generation: attempt},
			delivery.Outcome{Kind: state, Reason: reason},
			time.Now(),
		); err != nil {
			return err
		}
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
