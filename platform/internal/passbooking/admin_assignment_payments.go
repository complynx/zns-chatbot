package passbooking

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (s *snapshot) adminPayments(ctx context.Context, tx pgx.Tx, actor string, c AdminAssignment,
	targets []*Booking, before map[string]*Booking) error {
	for _, b := range targets {
		old := before[b.Owner]
		if old != nil && (old.State == assigned || old.State == paid) && c.TotalPrice == nil {
			// Metadata edits retain the financial timeline and attachment. Resetting
			// assigned_at alone would invalidate the pending receipt participant snapshot.
			continue
		}
		var attachment *string
		if b.Price != nil && *b.Price == 0 {
			id, err := s.freePayment(ctx, tx, actor, c, b)
			if err != nil {
				return err
			}
			attachment = &id
		}
		_, err := tx.Exec(
			ctx,
			`UPDATE core.pass_bookings SET payment_attempt=$3 WHERE event_id=$1 AND owner=$2`,
			c.Event,
			b.Owner,
			attachment,
		)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	return nil
}

func (s *snapshot) freePayment(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c AdminAssignment,
	b *Booking,
) (string, error) {
	if _, exists := s.event.admins[b.PaymentAdmin]; !exists {
		return "", conflict("pass_payment_admin_required")
	}
	id := hash([]byte("free\x00" + c.Event + "\x00" + actor + "\x00" + c.Key + "\x00" + b.Owner))
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.pass_payment_attempts(id,event_id,submitter,kind,proof_id,receiving_admin,received_at,decision,reviewed_by,reviewed_at)
 VALUES($1,$2,$3,'free',NULL,$4,$5,'accepted',$6,$5)`,
		id,
		c.Event,
		b.Owner,
		b.PaymentAdmin,
		s.now,
		actor,
	)
	if err != nil {
		return "", core.DatabaseOperationError(err)
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) VALUES($1,$2,$3)`,
		id,
		b.Owner,
		b.AssignedAt,
	)
	return id, core.DatabaseOperationError(err)
}
