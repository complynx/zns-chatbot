package passbooking

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/broadcastprofile"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

type adminProfile struct {
	version int64
	role    passallocation.Role
	name    string
	frozen  bool
}

func lockAdminProfile(ctx context.Context, tx pgx.Tx, c AdminAssignment) (adminProfile, error) {
	var profile adminProfile
	if c.Create == nil {
		return profile, nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO core.pass_profiles(owner) VALUES($1) ON CONFLICT DO NOTHING`, c.Target)
	if err != nil {
		return profile, core.DatabaseOperationError(err)
	}
	err = tx.QueryRow(ctx, `SELECT version,role,legal_name,frozen FROM core.pass_profiles WHERE owner=$1 FOR UPDATE`, c.Target).
		Scan(&profile.version, &profile.role, &profile.name, &profile.frozen)
	if err != nil {
		return profile, core.DatabaseOperationError(err)
	}
	if profile.version != c.Create.ProfileVersion {
		return profile, conflict("pass_profile_stale")
	}
	return profile, nil
}

func (s *snapshot) adminTargets(ctx context.Context, tx pgx.Tx, c AdminAssignment, p adminProfile) ([]*Booking, error) {
	b := s.bookings[c.Target]
	if b == nil || b.State == cancelled {
		var err error
		b, err = s.adminCreate(ctx, tx, c, p, b)
		if err != nil {
			return nil, err
		}
	} else if c.Create != nil {
		return nil, conflict("pass_booking_state")
	}
	return s.adminExistingTargets(b)
}

func (s *snapshot) adminExistingTargets(b *Booking) ([]*Booking, error) {
	if b.State == assigned || b.State == paid {
		if partner := s.bookings[b.Partner]; partner != nil && partner.Partner == b.Owner {
			s.unlink(partner)
		}
		if b.Partner != "" {
			s.unlink(b)
		}
		return []*Booking{b}, nil
	}
	if b.State != waitlist {
		return nil, conflict("pass_booking_state")
	}
	if b.Partner == "" {
		return []*Booking{b}, nil
	}
	partner := s.bookings[b.Partner]
	if partner != nil && partner.State == pending {
		return nil, conflict("pass_booking_state")
	}
	if partner == nil || partner.Partner != b.Owner || partner.State != waitlist {
		s.unlink(b)
		return []*Booking{b}, nil
	}
	if partner.Role == b.Role {
		return nil, conflict("pass_booking_state")
	}
	targets := []*Booking{b, partner}
	slices.SortFunc(targets, func(a, b *Booking) int {
		if a.TelegramID < b.TelegramID {
			return -1
		}
		if a.TelegramID > b.TelegramID {
			return 1
		}
		return 0
	})
	return targets, nil
}

func (s *snapshot) adminCreate(
	ctx context.Context,
	tx pgx.Tx,
	c AdminAssignment,
	p adminProfile,
	old *Booking,
) (*Booking, error) {
	if c.Create == nil {
		return nil, conflict("pass_booking_missing")
	}
	role := c.Create.Role
	if c.Create.FromProfile {
		role = p.role
		if role == "" {
			err := tx.QueryRow(ctx, `SELECT role FROM core.pass_bookings WHERE owner=$1 ORDER BY created_at DESC,event_id LIMIT 1`, c.Target).
				Scan(&role)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, core.DatabaseOperationError(err)
			}
		}
	}
	if role != passallocation.Leader && role != passallocation.Follower {
		return nil, conflict("pass_role_required")
	}
	if err := s.adminProfileName(ctx, tx, c, p); err != nil {
		return nil, err
	}
	var telegramID int64
	if err := tx.QueryRow(ctx, `SELECT telegram_id FROM core.users WHERE id=$1`, c.Target).
		Scan(&telegramID); err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	admins := []string{}
	for owner, hidden := range s.event.admins {
		if !hidden {
			admins = append(admins, owner)
		}
	}
	slices.Sort(admins)
	if len(admins) == 0 {
		return nil, conflict("pass_payment_admin_required")
	}
	b := &Booking{
		Event:        c.Event,
		Owner:        c.Target,
		TelegramID:   telegramID,
		Version:      bookingVersion(old),
		State:        waitlist,
		Role:         role,
		Kind:         solo,
		PaymentAdmin: admins[0],
		CreatedAt:    s.now,
	}
	s.bookings[b.Owner] = b
	return b, nil
}

func (s *snapshot) adminProfileName(ctx context.Context, tx pgx.Tx, c AdminAssignment, p adminProfile) error {
	if c.Create.LegalName == nil || *c.Create.LegalName == p.name {
		return nil
	}
	if p.frozen {
		return conflict("pass_profile_frozen")
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE core.pass_profiles SET legal_name=$2,version=version+1 WHERE owner=$1`,
		c.Target,
		*c.Create.LegalName,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_profile_history(owner,version,action,field,origin,at) VALUES($1,$2,'set','legal_name','manual',$3)`,
		c.Target,
		p.version+1,
		s.now,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return broadcastprofile.PassField(ctx, tx, c.Target, "legal_name", *c.Create.LegalName)
}
