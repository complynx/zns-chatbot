package massage

import (
	"context"

	"net/http"

	"github.com/jackc/pgx/v5"
)

// A shared role lock keeps membership current through the complete read.
func practitionerReadTx(ctx context.Context, s Service, actor, event string) (pgx.Tx, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	permitted, err := lockPractitionerRole(ctx, tx, actor, event)
	if err != nil || !permitted {
		_ = tx.Rollback(ctx)
		if err == nil {
			err = problem(http.StatusForbidden, "forbidden")
		}
		return nil, err
	}
	return tx, nil
}
