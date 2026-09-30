package passbooking

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const (
	firstReminderAfter  = 6 * 24 * time.Hour
	secondReminderAfter = 24 * time.Hour
	cancellationAfter   = 2 * 24 * time.Hour
	invitationAfter     = 58 * time.Hour
	deadlineEventBatch  = 100
)

type deadlineMarker struct {
	Owner  string
	First  *time.Time
	Second *time.Time
}

// ProcessDeadlines is trusted maintenance, not a user command. Every event uses
// the same row lock as booking mutations and reads the clock after acquiring it.
func (s Service) ProcessDeadlines(ctx context.Context) (int64, error) {
	var total int64
	var failures error
	failed := 0
	after := ""
	for {
		events, err := s.maintenanceEvents(ctx, after)
		if err != nil {
			return total, errors.Join(failures, err)
		}
		for _, id := range events {
			count, processErr := s.processEventDeadlines(ctx, id)
			if processErr == nil {
				total += count
				continue
			}
			if ctx.Err() != nil {
				return total, errors.Join(failures, processErr, ctx.Err())
			}
			failed++
			failures = errors.Join(failures, processErr)
		}
		if len(events) < deadlineEventBatch {
			break
		}
		// Keyset paging visits each event once, including queues that remain
		// blocked. They must not starve later events on every maintenance tick.
		after = events[len(events)-1]
	}
	if failures != nil {
		return total, fmt.Errorf("pass maintenance failed for %d events: %w", failed, failures)
	}
	return total, nil
}

func (s Service) maintenanceEvents(ctx context.Context, after string) ([]string, error) {
	rows, err := s.DB.Query(
		ctx,
		`SELECT DISTINCT e.id FROM core.pass_events e LEFT JOIN core.pass_bookings b ON b.event_id=e.id
 LEFT JOIN core.pass_deadline_markers m ON m.event_id=b.event_id AND m.owner=b.owner AND m.assigned_at=b.assigned_at
 WHERE e.id>$2 AND e.finishes_at>clock_timestamp() AND (
 EXISTS(SELECT 1 FROM core.registration_intents i WHERE i.event_id=e.id AND i.state='captured') OR
 EXISTS(SELECT 1 FROM core.registration_ingress g WHERE g.native_event=e.id AND g.native_payload IS NOT NULL AND g.native_outcome='') OR b.state='waitlist' OR
 (b.state='waiting-for-couple' AND COALESCE(b.invitation_started_at,b.created_at)<clock_timestamp()-interval '58 hours') OR
 (b.state='assigned' AND ((m.first_at IS NULL AND b.assigned_at<clock_timestamp()-interval '6 days') OR
 m.first_at<clock_timestamp()-interval '2 days' OR (m.second_at IS NULL AND m.first_at<clock_timestamp()-interval '1 day'))))
 ORDER BY e.id LIMIT $1`,
		deadlineEventBatch,
		after,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowTo[string])
	return events, core.DatabaseOperationError(err)
}

func (s Service) processEventDeadlines(ctx context.Context, id string) (int64, error) {
	if err := s.ResolveRegistrationIntake(ctx, id); err != nil {
		return 0, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	e, err := readEvent(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	bookings, err := readBookings(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	markers, err := readDeadlineMarkers(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	if !now.Before(e.finishes) {
		return 0, core.DatabaseOperationError(tx.Commit(ctx))
	}
	before := copyBookings(bookings)
	if err = refreshRegistrationTurns(ctx, tx, id, s.registrationRetention(), now); err != nil {
		return 0, err
	}
	state := newSnapshot(e, bookings, now)
	if err = state.loadRegistrationRanks(ctx, tx); err != nil {
		return 0, err
	}
	state.deliveryBotID = s.Delivery.BotID
	state.announcementBindings = s.AnnouncementBindings
	ordered := make([]string, 0, len(bookings))
	for owner := range bookings {
		ordered = append(ordered, owner)
	}
	slices.Sort(ordered)
	var count int64
	for _, owner := range ordered {
		changed, deadlineErr := state.processDeadline(ctx, tx, bookings[owner], markers[owner])
		if deadlineErr != nil {
			return 0, deadlineErr
		}
		if changed {
			count++
		}
	}
	state.allocate()
	if err = persist(ctx, tx, state); err != nil {
		return 0, err
	}
	if err = state.notifyChanges(ctx, tx, before, "deadline"); err != nil {
		return 0, err
	}
	return count, core.DatabaseOperationError(tx.Commit(ctx))
}

func readDeadlineMarkers(ctx context.Context, tx pgx.Tx, event string) (map[string]deadlineMarker, error) {
	rows, err := tx.Query(ctx, `SELECT m.owner,m.first_at,m.second_at FROM core.pass_deadline_markers m
 JOIN core.pass_bookings b ON b.event_id=m.event_id AND b.owner=m.owner AND b.assigned_at=m.assigned_at WHERE m.event_id=$1`, event)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	markers, err := pgx.CollectRows(rows, pgx.RowToStructByPos[deadlineMarker])
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	result := make(map[string]deadlineMarker, len(markers))
	for _, marker := range markers {
		result[marker.Owner] = marker
	}
	return result, nil
}

func (s *snapshot) processDeadline(ctx context.Context, tx pgx.Tx, b *Booking, marker deadlineMarker) (bool, error) {
	if b.State == pending && b.invitationStart().Before(s.now.Add(-invitationAfter)) {
		return true, s.expireInvitation(ctx, tx, b)
	}
	if b.State != assigned || b.AssignedAt == nil {
		return false, nil
	}
	if marker.First != nil && marker.First.Before(s.now.Add(-cancellationAfter)) {
		partner := s.bookings[b.Partner]
		// A historical mixed paid/unpaid pair must never cancel the paid participant.
		return true, s.cancel(b, partner != nil && partner.State == paid)
	}
	if marker.First == nil && b.AssignedAt.Before(s.now.Add(-firstReminderAfter)) {
		_, err := tx.Exec(
			ctx,
			`INSERT INTO core.pass_deadline_markers(event_id,owner,assigned_at,first_at) VALUES($1,$2,$3,$4) ON CONFLICT(event_id,owner,assigned_at) DO UPDATE SET first_at=EXCLUDED.first_at`,
			b.Event,
			b.Owner,
			b.AssignedAt,
			s.now,
		)
		if err != nil {
			return false, core.DatabaseOperationError(err)
		}
		return true, enqueuePassNotice(ctx, tx, s.deliveryBotID, &s.notificationRegistrations,
			b,
			b.Owner,
			"reminder_first",
			noticeTime(*b.AssignedAt),
			"",
		)
	}
	if marker.Second == nil && marker.First != nil && marker.First.Before(s.now.Add(-secondReminderAfter)) {
		_, err := tx.Exec(
			ctx,
			`UPDATE core.pass_deadline_markers SET second_at=$4 WHERE event_id=$1 AND owner=$2 AND assigned_at=$3`,
			b.Event,
			b.Owner,
			b.AssignedAt,
			s.now,
		)
		if err != nil {
			return false, core.DatabaseOperationError(err)
		}
		return true, enqueuePassNotice(ctx, tx, s.deliveryBotID, &s.notificationRegistrations,
			b,
			b.Owner,
			"reminder_second",
			noticeTime(*b.AssignedAt),
			"",
		)
	}
	return false, nil
}

func (s *snapshot) expireInvitation(ctx context.Context, tx pgx.Tx, b *Booking) error {
	invited := b.InvitationTarget
	b.State = waitlist
	b.Kind = solo
	b.InvitationTarget = 0
	b.InvitationStartedAt = nil
	b.Partner = ""
	s.touch(b)
	if err := enqueuePassNotice(ctx, tx, s.deliveryBotID, &s.notificationRegistrations,
		b,
		b.Owner,
		"invitation_expired",
		noticeVersion(b),
		"",
	); err != nil {
		return err
	}
	return notifyKnownInvitee(ctx, tx, s.deliveryBotID, &s.notificationRegistrations, b, invited, "invitation_expired")
}

// Historical and imported rows without an invitation start retain their legacy
// deadline. Their registration time is not claimed as a known invitation start.
func (b Booking) invitationStart() time.Time {
	if b.InvitationStartedAt != nil {
		return *b.InvitationStartedAt
	}
	return b.CreatedAt
}
