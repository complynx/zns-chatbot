package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

// PaymentReviewPayer contains display data only, never private registration fields.
type PaymentReviewPayer struct {
	UserID   uuid.UUID `json:"user_id"`
	FullName string    `json:"full_name"`
}

// MiniAppPaymentReviewPayer derives the owner from evidence after checking the
// current event authority and session together. It does not accept a target user.
func (s *Store) MiniAppPaymentReviewPayer(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, eventID, evidenceID uuid.UUID,
) (PaymentReviewPayer, error) {
	if !paymentScopeValid(scope, "read") || eventID == uuid.Nil || evidenceID == uuid.Nil {
		return PaymentReviewPayer{}, payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewPayer{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := sqlc.New(tx)
	if _, _, err = lockPaymentEvent(ctx, q, scope, eventID, true); err != nil {
		return PaymentReviewPayer{}, err
	}
	if err = (paymentReviewOrigin{session: &session}).authorize(ctx, tx, scope); err != nil {
		return PaymentReviewPayer{}, err
	}
	row, err := q.ReadPaymentReviewPayer(ctx, sqlc.ReadPaymentReviewPayerParams{
		ID: evidenceID, EventID: eventID, BotID: scope.BotID, ZitadelOrg: scope.Organization,
	})
	if err != nil {
		return PaymentReviewPayer{}, err
	}
	return PaymentReviewPayer{UserID: row.UserID, FullName: row.FullName}, nil
}
