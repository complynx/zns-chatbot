package orders

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// LockDeliveryExportInTx retains the current export grant and actor binding.
func LockDeliveryExportInTx(ctx context.Context, tx pgx.Tx, actor, event string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT a.owner FROM core.order_admins a JOIN core.users u ON u.id=a.owner WHERE a.event_id=$1 AND a.owner=$2 AND u.can_book FOR SHARE OF a,u`, event, actor).
		Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusForbidden, "forbidden")
	}
	return err
}

// LockDeliveryProofInTx validates the exact attachment/version under row locks.
func LockDeliveryProofInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, event, id string,
	version int64,
	attempt string,
) error {
	var owner string
	if err := tx.QueryRow(ctx, `SELECT owner FROM core.orders WHERE id=$1 AND event_id=$2`, id, event).
		Scan(&owner); err != nil {
		return err
	}
	if owner != actor {
		if err := LockDeliveryExportInTx(ctx, tx, actor, event); err != nil {
			return err
		}
	}
	var current Proof
	err := tx.QueryRow(ctx, `SELECT p.id,p.filename,o.version,o.attempt `+orderProofScope+` FOR SHARE OF o,p,u`, actor, event, id).
		Scan(&current.ID, &current.Filename, &current.Version, &current.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusNotFound, "proof_not_found")
	}
	if err != nil {
		return err
	}
	if current.Version != version || current.Attempt != attempt {
		return problem(http.StatusConflict, "proof_stale")
	}
	return nil
}

// LockDeliveryPaymentInTx authorizes the owner's payment instructions.
func LockDeliveryPaymentInTx(ctx context.Context, tx pgx.Tx, actor, event, id string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT id FROM core.orders WHERE id=$1 AND owner=$2 AND event_id=$3 AND state<>'deleted' FOR SHARE`, id, actor, event).
		Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusNotFound, "order_not_found")
	}
	return err
}
