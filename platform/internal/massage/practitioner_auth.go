package massage

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// A shared role lock keeps membership current through the complete read.
func practitionerReadTx(ctx context.Context, s Service, actor, event string) (pgx.Tx, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT owner FROM core.massage_specialists WHERE event_id=$1 AND owner=$2 FOR SHARE`, event, actor).
		Scan(&owner)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, problem(http.StatusForbidden, "forbidden")
		}
		return nil, err
	}
	return tx, nil
}
