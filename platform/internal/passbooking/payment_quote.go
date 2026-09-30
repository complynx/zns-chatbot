package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type PaymentQuote struct {
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
	Version  int64  `json:"version"`
}

// PaymentQuote uses actual per-participant amounts, which may differ in a pair.
// A pending invitation or a broken pair is not a payable couple.
func (s Service) PaymentQuote(ctx context.Context, actor, eventID string) (PaymentQuote, error) {
	quote := PaymentQuote{Currency: "RUB"}
	err := s.DB.QueryRow(ctx, `SELECT b.price::bigint+COALESCE(p.price::bigint,0),b.version
FROM core.pass_bookings b LEFT JOIN core.pass_bookings p
 ON p.event_id=b.event_id AND p.owner=b.partner AND p.partner=b.owner AND p.state='assigned'
WHERE b.event_id=$1 AND b.owner=$2 AND b.state='assigned' AND b.price IS NOT NULL
AND (b.partner='' OR p.owner IS NOT NULL)
AND EXISTS(SELECT 1 FROM core.users u WHERE u.id=$2 AND u.can_book)`, eventID, actor).
		Scan(&quote.Total, &quote.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentQuote{}, conflict("pass_payment_state")
	}
	return quote, core.DatabaseOperationError(err)
}
