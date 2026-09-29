package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const CommandTakeover = "takeover"
const CommandReceivedOnly = "received_only"

func authorizeTakeover(ctx context.Context, tx pgx.Tx, actor, eventID string) error {
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).Scan(&owner)
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT owner FROM core.pass_payment_admins WHERE owner=$1 AND event_id=$2 FOR SHARE`, actor, eventID).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	return err
}

func (s *snapshot) takeover(ctx context.Context, tx pgx.Tx, actor string, c Command) error {
	target := s.bookings[c.Target]
	if target == nil || target.Version != c.TargetVersion {
		return conflict("pass_booking_stale")
	}
	if c.Name == CommandReceivedOnly && target.State != paid {
		return conflict("pass_payment_state")
	}
	participants := []*Booking{target}
	if target.Partner != "" {
		partner := s.bookings[target.Partner]
		if partner == nil || partner.Partner != target.Owner {
			return conflict("pass_booking_state")
		}
		participants = append(participants, partner)
	}
	for _, b := range participants {
		if c.Name == CommandReceivedOnly {
			if err := s.backfillReceiver(ctx, tx, b, actor); err != nil {
				return err
			}
		} else if b.PaymentAdmin != actor {
			b.PaymentAdmin = actor
			s.touch(b)
		}
	}
	return nil
}

func (s *snapshot) backfillReceiver(ctx context.Context, tx pgx.Tx, b *Booking, actor string) error {
	if b.State != paid || b.AssignedAt == nil {
		return nil
	}
	result, err := tx.Exec(
		ctx,
		`INSERT INTO core.pass_receiver_backfills(event_id,owner,assigned_at,legacy_receiving_admin,recorded_at)
 SELECT event_id,owner,assigned_at,$3,$4 FROM core.pass_bookings
 WHERE event_id=$1 AND owner=$2 AND payment_attempt IS NULL
 ON CONFLICT(event_id,owner,assigned_at) DO NOTHING`,
		b.Event,
		b.Owner,
		actor,
		s.now,
	)
	if err == nil && result.RowsAffected() > 0 {
		s.touch(b)
	}
	return err
}
