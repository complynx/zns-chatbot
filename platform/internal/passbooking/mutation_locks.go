package passbooking

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LockMutationEvents takes the strongest lock for the sorted source/target union,
// avoiding upgrades when two commands consume each other's registration event.
// It does not authorize missing or present events.
func LockMutationEvents(ctx context.Context, tx pgx.Tx, events []string) error {
	_, err := tx.Exec(
		ctx,
		`SELECT id FROM core.pass_events WHERE id=ANY($1::text[]) ORDER BY id FOR NO KEY UPDATE`,
		events,
	)
	return err
}
