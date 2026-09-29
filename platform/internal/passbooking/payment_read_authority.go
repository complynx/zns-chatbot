package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func lockPaymentReadRole(ctx context.Context, tx pgx.Tx, actor, event string) (bool, error) {
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_payment_admins WHERE event_id=$1 AND owner=$2 FOR SHARE`, event, actor).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
