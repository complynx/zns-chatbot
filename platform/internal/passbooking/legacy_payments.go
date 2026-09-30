package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const paymentAccepted = "accepted"

// Legacy free assignments have no fabricated uploader or payment attempt.
func (s Service) legacyFreePayment(ctx context.Context, actor, eventID, owner string) (Payment, error) {
	payment := Payment{Kind: "free", Event: eventID, Decision: paymentAccepted}
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(m.receiving_admin,''),m.received_at,m.accepted_at,b.version,m.reviewed_by
 FROM core.pass_bookings b JOIN core.legacy_pass_payment_metadata m
 ON m.event_id=b.event_id AND m.owner=b.owner AND m.assigned_at=b.assigned_at
 WHERE b.event_id=$1 AND b.owner=$2 AND b.state='paid' AND b.price=0 AND b.payment_attempt IS NULL
 AND m.proof_reference='free_pass'
 AND (b.owner=$3 OR EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=b.event_id AND a.owner=$3))`, eventID, owner, actor).
		Scan(&payment.ReceivingAdmin, &payment.ReceivedAt, &payment.ReviewedAt, &payment.Version, &payment.ReviewedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, forbidden()
	}
	return payment, core.DatabaseOperationError(err)
}
