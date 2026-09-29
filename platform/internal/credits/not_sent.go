package credits

import "context"

// NotSent records a host-confirmed pre-send denial. It cannot replace settlement
// or release a possibly sent request; the provider adapter owns that proof.
func (s Service) NotSent(ctx context.Context, id string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var payer string
	if err = tx.QueryRow(ctx, `SELECT payer FROM credits.attempts WHERE id=$1`, id).Scan(&payer); err != nil {
		return err
	}
	if _, err = lockPolicy(ctx, tx, payer); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE credits.attempts SET state='not_sent',dispatched_at=NULL
 WHERE id=$1 AND state IN ('reserved','dispatched','not_sent') AND usage IS NULL AND usage_sha256 IS NULL
 AND cost_nano_usd IS NULL AND settled_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
