package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func (s *snapshot) notifyChanges(ctx context.Context, tx pgx.Tx, before map[string]*Booking, cause string) error {
	for owner := range s.dirty {
		b, old := s.bookings[owner], before[owner]
		for _, kind := range changedNotices(b, old, cause) {
			generation := noticeVersion(b)
			if kind == "registered" {
				generation = noticeTime(b.CreatedAt)
			}
			if err := enqueuePassNotice(
				ctx,
				tx,
				s.deliveryBotID,
				&s.notificationRegistrations,
				b,
				b.Owner,
				kind,
				generation,
				"",
			); err != nil {
				return err
			}
		}
		if b.State == pending && (old == nil || old.State != pending || old.InvitationTarget != b.InvitationTarget) {
			if err := notifyKnownInvitee(
				ctx,
				tx,
				s.deliveryBotID,
				&s.notificationRegistrations,
				b,
				b.InvitationTarget,
				"invitation",
			); err != nil {
				return err
			}
		}
	}
	if err := s.notifyWaitlist(ctx, tx); err != nil {
		return err
	}
	announcements, err := enqueueRegistrationAnnouncements(ctx, tx, s.event.id, s.deliveryBotID, s.announcementBindings)
	if err != nil {
		return err
	}
	return delivery.RegisterBatch(ctx, tx, s.deliveryBotID, append(s.notificationRegistrations, announcements...))
}

func (s *snapshot) notifyWaitlist(ctx context.Context, tx pgx.Tx) error {
	// Recalculation must also notify previously imported or unnotified waitlists.
	// Their booking version does not change merely because a notice is queued.
	for _, b := range s.bookings {
		if b.State == waitlist {
			if err := enqueuePassNotice(ctx, tx, s.deliveryBotID, &s.notificationRegistrations,
				b,
				b.Owner,
				"waitlisted",
				noticeTime(b.CreatedAt),
				"",
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *snapshot) persistNotified(ctx context.Context, tx pgx.Tx, before map[string]*Booking, cause string) error {
	if err := persist(ctx, tx, s); err != nil {
		return err
	}
	return s.notifyChanges(ctx, tx, before, cause)
}

func changedNotices(b, old *Booking, cause string) []string {
	kinds := []string{}
	if cause == CommandTakeover && old != nil && b.PaymentAdmin != old.PaymentAdmin {
		kinds = append(kinds, "payment_contact_changed")
	}
	if old == nil || old.State == cancelled {
		if b.State != cancelled {
			kinds = append(kinds, "registered")
		}
	}
	kinds = append(kinds, pairNotices(b, old, cause)...)
	if kind := assignedNotice(b, old); kind != "" {
		kinds = append(kinds, kind)
	}
	if b.State == cancelled && old != nil && old.State != cancelled {
		kind := "cancelled"
		if cause == "deadline" {
			kind = "deadline_cancelled"
		}
		kinds = append(kinds, kind)
	}
	return kinds
}

func pairNotices(b, old *Booking, cause string) []string {
	kinds := []string{}
	if b.Partner != "" && (old == nil || old.Partner != b.Partner) {
		kinds = append(kinds, "pair_accepted")
	}
	if old != nil {
		if old.Partner != "" && b.Partner == "" && b.State != cancelled {
			kinds = append(kinds, "pair_changed")
		}
		if cause == commandDecline && old.State == pending && b.State != pending {
			kinds = append(kinds, "pair_declined")
		}
	}
	return kinds
}

func assignedNotice(b, old *Booking) string {
	if b.AssignedAt == nil || (old != nil && old.AssignedAt != nil && b.AssignedAt.Equal(*old.AssignedAt)) {
		return ""
	}
	if b.State == assigned {
		return "assigned"
	}
	if b.State == paid && b.Price != nil && *b.Price == 0 {
		return "free_assigned"
	}
	return ""
}

func notifyKnownInvitee(
	ctx context.Context,
	tx pgx.Tx,
	botID int64,
	pending *[]delivery.Registration,
	b *Booking,
	telegramID int64,
	kind string,
) error {
	var owner string
	err := tx.QueryRow(ctx, `SELECT id FROM core.users WHERE telegram_id=$1`, telegramID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return enqueuePassNotice(ctx, tx, botID, pending, b, owner, kind, noticeVersion(b), "")
}

func notifyPaymentRequest(
	ctx context.Context,
	tx pgx.Tx,
	botID int64,
	pending *[]delivery.Registration,
	b *Booking,
	attempt string,
) error {
	return enqueuePassNotice(ctx, tx, botID, pending, b, b.PaymentAdmin, "payment_request", attempt, attempt)
}

func notifyPaymentDecision(
	ctx context.Context,
	tx pgx.Tx,
	botID int64,
	pending *[]delivery.Registration,
	b *Booking,
	c Command,
) error {
	kind := "payment_accepted"
	if c.Name == commandProofReject {
		kind = "payment_rejected"
	}
	return enqueuePassNotice(ctx, tx, botID, pending, b, b.Owner, kind, c.PaymentAttempt, c.PaymentAttempt)
}
