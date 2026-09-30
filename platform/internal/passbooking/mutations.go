package passbooking

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func (s *snapshot) mutate(ctx context.Context, tx pgx.Tx, b *Booking, c Command) error {
	switch c.Name {
	case "solo", commandInvite:
		return s.register(ctx, tx, b, c)
	case commandAccept, commandDecline:
		return s.respond(ctx, tx, b, c)
	case "cancel":
		return s.cancel(b, false)
	case commandProof:
		return s.submitProof(ctx, tx, b, c)
	case commandProofAccept, commandProofReject:
		return s.reviewProof(ctx, tx, b.Owner, c)
	case CommandTakeover, CommandReceivedOnly:
		return s.takeover(ctx, tx, b.Owner, c)
	case "payment_admin":
		if b.Version == 0 || b.State == paid || b.State == cancelled {
			return conflict("pass_booking_state")
		}
		if hidden, exists := s.event.admins[c.PaymentAdmin]; !exists || hidden {
			return invalid()
		}
		b.PaymentAdmin = c.PaymentAdmin
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.pass_contact_preferences(event_id,owner,payment_admin) VALUES($1,$2,$3) ON CONFLICT(event_id,owner) DO UPDATE SET payment_admin=EXCLUDED.payment_admin`,
			s.event.id,
			b.Owner,
			b.PaymentAdmin,
		); err != nil {
			return core.DatabaseOperationError(err)
		}
		s.touch(b)
	case commandAdminCancel, "admin_uncouple":
		target := s.bookings[c.Target]
		if target == nil || target.Version != c.TargetVersion {
			return conflict("pass_booking_stale")
		}
		if c.Name == commandAdminCancel {
			return s.cancel(target, true)
		}
		partner := s.bookings[target.Partner]
		if target.Partner == "" || partner == nil || partner.Partner != target.Owner {
			return conflict("pass_booking_state")
		}
		s.unlink(target)
		s.unlink(partner)
	case commandRecalculate:
	default:
		return invalid()
	}
	return nil
}

func (s *snapshot) identity(ctx context.Context, tx pgx.Tx, owner string) (passallocation.Role, error) {
	var role passallocation.Role
	var name, passport string
	err := tx.QueryRow(ctx, `SELECT role,legal_name,passport FROM core.pass_profiles WHERE owner=$1 FOR SHARE`, owner).
		Scan(&role, &name, &passport)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", conflict("pass_profile_required")
	}
	if err != nil {
		return "", core.DatabaseOperationError(err)
	}
	if s.event.passport && (name == "" || passport == "") {
		return "", conflict("pass_identity_required")
	}
	return role, nil
}

func (s *snapshot) open() bool {
	if !s.now.Before(s.event.finishes) {
		return false
	}
	for _, tier := range s.event.tiers {
		if !s.now.Before(tier.Start) {
			return true
		}
	}
	return false
}

func editable(b *Booking) bool {
	return b.Version == 0 || b.State == cancelled || b.State == pending || (b.State == waitlist && b.Partner == "")
}

func (s *snapshot) register(ctx context.Context, tx pgx.Tx, b *Booking, c Command) error {
	if !editable(b) {
		return conflict("pass_booking_state")
	}
	if (b.Version == 0 || b.State == cancelled) && !s.open() {
		return conflict("pass_sales_closed")
	}
	if !s.now.Before(s.event.finishes) {
		return conflict("pass_event_finished")
	}
	role, err := s.identity(ctx, tx, b.Owner)
	if err != nil {
		return err
	}
	newRegistration := b.Version == 0 || b.State == cancelled
	if newRegistration {
		if err = s.initializeRegistration(ctx, tx, b, role, c.PaymentAdmin); err != nil {
			return err
		}
	}
	if err = s.chooseAdmin(b, newRegistration); err != nil {
		return err
	}
	sameInvitation := b.State == pending && b.InvitationTarget == c.InviteTelegramID
	invitationStartedAt := b.InvitationStartedAt
	b.State = waitlist
	b.Kind = solo
	b.InvitationTarget = 0
	b.InvitationStartedAt = nil
	b.Partner = ""
	b.AssignedAt = nil
	b.Price = nil
	b.TierIndex = nil
	b.SkipBalance = nil
	if c.Name == commandInvite {
		if err = s.invite(b, c.InviteTelegramID); err != nil {
			return err
		}
		// Repeating the same active invitation must not extend its window.
		if sameInvitation {
			b.InvitationStartedAt = invitationStartedAt
		}
	}
	s.bookings[b.Owner] = b
	s.touch(b)
	return nil
}

func (s *snapshot) initializeRegistration(
	ctx context.Context, tx pgx.Tx, b *Booking, role passallocation.Role, admin string,
) error {
	if role != passallocation.Leader && role != passallocation.Follower {
		return conflict("pass_role_required")
	}
	b.Role = role
	b.CreatedAt = s.now
	b.PaymentAdmin = admin
	if admin != "" {
		return nil
	}
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT p.payment_admin FROM core.pass_contact_preferences p
 JOIN core.pass_payment_admins a ON a.event_id=p.event_id AND a.owner=p.payment_admin
 WHERE p.event_id=$1 AND p.owner=$2 AND NOT a.hidden),'')`, s.event.id, b.Owner).Scan(&b.PaymentAdmin)
	return core.DatabaseOperationError(err)
}

func (s *snapshot) chooseAdmin(b *Booking, newRegistration bool) error {
	hidden, exists := s.event.admins[b.PaymentAdmin]
	if exists && (!newRegistration || !hidden) {
		return nil
	}
	choices := []string{}
	for owner, isHidden := range s.event.admins {
		if !isHidden {
			choices = append(choices, owner)
		}
	}
	slices.Sort(choices)
	if len(choices) != 1 {
		return conflict("pass_payment_admin_required")
	}
	b.PaymentAdmin = choices[0]
	return nil
}

func (s *snapshot) invite(b *Booking, target int64) error {
	if target == 0 || target == b.TelegramID {
		return invalid()
	}
	for _, candidate := range s.bookings {
		if candidate.TelegramID == target && !editable(candidate) {
			return conflict("pass_invitee_unavailable")
		}
	}
	b.State = pending
	b.Kind = couple
	b.InvitationTarget = target
	b.InvitationStartedAt = &s.now
	return nil
}

func (s *snapshot) respond(ctx context.Context, tx pgx.Tx, b *Booking, c Command) error {
	inviter := s.bookings[c.Target]
	if inviter == nil || inviter.Version != c.TargetVersion || inviter.State != pending ||
		inviter.InvitationTarget != b.TelegramID {
		return conflict("pass_invitation_stale")
	}
	if c.Name == commandDecline {
		inviter.State = waitlist
		inviter.Kind = solo
		inviter.InvitationTarget = 0
		inviter.InvitationStartedAt = nil
		s.touch(inviter)
		return nil
	}
	if !s.now.Before(s.event.finishes) {
		return conflict("pass_event_finished")
	}
	if !editable(b) {
		return conflict("pass_booking_state")
	}
	if s.event.passport {
		if _, err := s.identity(ctx, tx, b.Owner); err != nil {
			return err
		}
	}
	version := b.Version
	owner, telegramID := b.Owner, b.TelegramID
	*b = *inviter
	b.Owner = owner
	b.TelegramID = telegramID
	b.Version = version
	b.State = waitlist
	b.Partner = inviter.Owner
	b.InvitationTarget = 0
	b.InvitationStartedAt = nil
	b.Role = passallocation.Leader
	if inviter.Role == passallocation.Leader {
		b.Role = passallocation.Follower
	}
	inviter.State = waitlist
	inviter.Partner = b.Owner
	inviter.InvitationTarget = 0
	inviter.InvitationStartedAt = nil
	s.bookings[b.Owner] = b
	s.touch(b)
	s.touch(inviter)
	return nil
}

func (s *snapshot) unlink(b *Booking) { b.Partner = ""; b.Kind = solo; s.touch(b) }

func (s *snapshot) cancel(b *Booking, admin bool) error {
	if b.Version == 0 || b.State == cancelled || (!admin && b.State == paid) {
		return conflict("pass_booking_state")
	}
	partner := s.bookings[b.Partner]
	if partner != nil && partner.Partner == b.Owner {
		if !admin && partner.State == paid {
			return conflict("pass_booking_state")
		}
		s.unlink(partner)
		if !admin {
			s.tombstone(partner)
		}
	}
	s.tombstone(b)
	return nil
}

func (s *snapshot) tombstone(b *Booking) {
	s.unlink(b)
	b.State = cancelled
	b.InvitationTarget = 0
	b.InvitationStartedAt = nil
	b.AssignedAt = nil
	b.Price = nil
	b.TierIndex = nil
}
