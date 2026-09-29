package orders

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Tokenless cash can survive capacity-only version changes after import. Its
// durable original payment facts identify the compatibility case; normal actor,
// current version and action authorization still run before this check.
func (op *operation) checkPaymentAttempt(ctx context.Context, order *Order) error {
	if op.command.Attempt != order.Attempt {
		return problem(http.StatusConflict, "stale_attempt")
	}
	if order.Attempt != "" {
		return nil
	}
	if order.State != stateCash || !op.command.isAdmin() {
		return problem(http.StatusConflict, "stale_attempt")
	}
	var preservePriority bool
	err := op.tx.QueryRow(ctx, `SELECT source_record ? 'payment_attempt_created_at' OR source_record ? 'proof_received'
	FROM core.legacy_order_import_references WHERE event_id=$1 AND target_id=$2 AND source_domain='orders'
	AND NOT source_record ? 'payment_attempt_token' AND source_record->'validation' IS DISTINCT FROM 'true'::jsonb
	AND (source_record->>'proof_file'='cash' OR
	(NOT source_record ? 'proof_file' AND source_record ? 'cash_requested_at'))`, order.EventID, order.ID).
		Scan(&preservePriority)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusConflict, "stale_attempt")
	}
	if err != nil {
		return err
	}
	if op.command.Name == actionAccept {
		// This confirmation is a new host action, not an invented legacy token.
		order.Attempt = token()
		if !preservePriority {
			order.AttemptAt = &op.now
		}
	}
	return nil
}
