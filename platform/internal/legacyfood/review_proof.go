package legacyfood

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// ReviewProof reads only the observed current payment. Version, generation and
// live reviewer authority are checked in the same statement that loads bytes.
// The ordinary Proof method retains historical owner/manual access semantics.
func (s Service) ReviewProof(
	ctx context.Context,
	actor, event, id, kind string,
	generation, version int64,
) (orders.Proof, error) {
	var proof orders.Proof
	if _, err := s.ReviewView(ctx, actor, event, id); err != nil {
		return proof, err
	}
	err := s.DB.QueryRow(ctx, `SELECT p.id,p.filename,p.body FROM core.food_orders o
 JOIN core.food_events e ON e.event_id=o.event_id AND e.bot_id=$7
 JOIN core.food_payments f ON f.order_id=o.id AND f.kind=$3 AND f.generation=$4
 JOIN core.order_proofs p ON p.id=f.proof_id AND p.owner=o.owner
 JOIN core.users u ON u.id=$6 AND u.can_book
 JOIN core.food_admins a ON a.event_id=o.event_id AND a.owner=u.id AND a.can_review
 WHERE o.event_id=$1 AND o.id=$2 AND o.version=$5
 AND f.generation=(SELECT max(n.generation) FROM core.food_payments n WHERE n.order_id=o.id AND n.kind=f.kind)`,
		event, id, kind, generation, version, actor, s.BotID).Scan(&proof.ID, &proof.Filename, &proof.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, problem("food_stale_payment")
	}
	return proof, core.DatabaseOperationError(err)
}
