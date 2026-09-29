package legacyfood

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// DeliveryRead describes the existing view/export/proof contract, not a grant.
type DeliveryRead struct {
	Event, Order, Kind, Scope string
	Generation, Version       int64
}

func (s Service) LockDeliveryReadInTx(ctx context.Context, tx pgx.Tx, actor string, in DeliveryRead) error {
	if err := allowed(ctx, tx, actor); err != nil {
		return err
	}
	if _, err := s.event(ctx, tx, in.Event, true); err != nil {
		return err
	}
	if in.Scope != "" {
		if err := adminPermission(ctx, tx, actor, in.Event, in.Scope, true); err != nil {
			return err
		}
	}
	if in.Order == "" {
		return nil
	}
	var owner string
	var version int64
	err := tx.QueryRow(ctx, `SELECT owner,version FROM core.food_orders WHERE event_id=$1 AND id=$2 FOR SHARE`, in.Event, in.Order).
		Scan(&owner, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem("food_order_not_found")
	}
	if err != nil {
		return err
	}
	if owner != actor && in.Scope == "" {
		if err = adminPermission(ctx, tx, actor, in.Event, "review", true); err != nil {
			return err
		}
	}
	if in.Version > 0 && version != in.Version {
		return problem("food_stale_payment")
	}
	if in.Kind == "" {
		return nil
	}
	var available bool
	err = tx.QueryRow(ctx, `SELECT p.id IS NOT NULL FROM core.food_payments f JOIN core.order_proofs p ON p.id=f.proof_id AND p.owner=$4 WHERE f.order_id=$1 AND f.kind=$2 AND f.generation=$3 FOR SHARE OF f,p`, in.Order, in.Kind, in.Generation, owner).
		Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem("food_proof_unavailable")
	}
	if err != nil {
		return err
	}
	if !available {
		return problem("food_proof_unavailable")
	}
	if in.Scope == "review" {
		var latest int64
		if err = tx.QueryRow(ctx, `SELECT max(generation) FROM core.food_payments WHERE order_id=$1 AND kind=$2`, in.Order, in.Kind).
			Scan(&latest); err != nil {
			return err
		}
		if latest != in.Generation {
			return problem("food_stale_payment")
		}
	}
	return nil
}
