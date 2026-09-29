package passbooking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/jackc/pgx/v5"
)

type PaymentReview struct {
	Owner      string  `json:"owner"`
	TelegramID int64   `json:"telegram_id"`
	Payment    Payment `json:"payment"`
}

type PaymentPage struct {
	Items []PaymentReview `json:"items"`
	Next  string          `json:"next,omitempty"`
}

// PaymentQueue returns each pending attempt once, using a current participant
// as its versioned review target. Opaque IDs give stable deletion-safe paging.
func (s Service) PaymentQueue(ctx context.Context, actor, eventID, after string) (PaymentPage, error) {
	if after != "" {
		if len(after) != sha256.Size*2 {
			return PaymentPage{}, invalid()
		}
		decoded, err := hex.DecodeString(after)
		if err != nil || len(decoded) != sha256.Size {
			return PaymentPage{}, invalid()
		}
	}
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE event_id=$1 AND owner=$2)`, eventID, actor).
		Scan(&allowed)
	if err != nil {
		return PaymentPage{}, err
	}
	if !allowed {
		return PaymentPage{}, forbidden()
	}
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT ON (p.id) b.owner,u.telegram_id,
p.kind,p.id,p.event_id,p.submitter,COALESCE(p.proof_id,''),COALESCE(legacy.receiving_admin,p.receiving_admin,''),p.received_at,p.decision,COALESCE(p.reviewed_by,legacy.reviewed_by),p.reviewed_at,b.version,p.proof_unavailable
FROM core.pass_payment_attempts p JOIN core.pass_bookings b ON b.payment_attempt=p.id AND b.event_id=p.event_id
JOIN core.users u ON u.id=b.owner
LEFT JOIN core.legacy_pass_payment_metadata legacy ON p.legacy_source_key IS NOT NULL
 AND legacy.event_id=b.event_id AND legacy.owner=b.owner AND legacy.assigned_at=b.assigned_at
 AND legacy.received_at=p.received_at
WHERE p.event_id=$1 AND p.decision='pending' AND b.state='paid' AND p.id>$3
AND EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=$1 AND a.owner=$2)
ORDER BY p.id,u.telegram_id LIMIT $4`, eventID, actor, after, pageSize+1)
	if err != nil {
		return PaymentPage{}, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PaymentReview, error) {
		var item PaymentReview
		p := &item.Payment
		scanErr := row.Scan(
			&item.Owner,
			&item.TelegramID,
			&p.Kind,
			&p.Attempt,
			&p.Event,
			&p.Submitter,
			&p.ProofID,
			&p.ReceivingAdmin,
			&p.ReceivedAt,
			&p.Decision,
			&p.ReviewedBy,
			&p.ReviewedAt,
			&p.Version,
			&p.ProofUnavailable,
		)
		return item, scanErr
	})
	if err != nil {
		return PaymentPage{}, err
	}
	page := PaymentPage{Items: items}
	if len(items) > pageSize {
		page.Next = items[pageSize-1].Payment.Attempt
		page.Items = items[:pageSize]
	}
	return page, nil
}
