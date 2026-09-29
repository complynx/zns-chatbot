package legacyfood

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// Proof authorizes against the food owner and current scoped review grant. The
// shared blob table is storage only; orders.OrderProof has a different ACL.
func (s Service) Proof(ctx context.Context, actor, event, id, kind string, generation int64) (orders.Proof, error) {
	order, err := s.Get(ctx, actor, event, id)
	if err != nil {
		return orders.Proof{}, err
	}
	if _, err = payment(&order, kind); err != nil {
		return orders.Proof{}, err
	}
	var proof orders.Proof
	err = s.DB.QueryRow(ctx, `SELECT p.id,p.filename,p.body FROM core.food_payments f
 JOIN core.order_proofs p ON p.id=f.proof_id AND p.owner=$4
 WHERE f.order_id=$1 AND f.kind=$2 AND f.generation=$3`, id, kind, generation, order.Owner).Scan(&proof.ID, &proof.Filename, &proof.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return proof, problem("food_proof_unavailable")
	}
	return proof, err
}
