package fence

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// LockOwners orders history fences across an output owner and causal origins.
func LockOwners(ctx context.Context, tx pgx.Tx, owners []string) error {
	ordered := append([]string{}, owners...)
	slices.Sort(ordered)
	for _, owner := range slices.Compact(ordered) {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO core.conversation_summaries(owner) VALUES($1) ON CONFLICT DO NOTHING`,
			owner,
		); err != nil {
			return core.DatabaseOperationError(err)
		}
		if _, err := tx.Exec(
			ctx,
			`SELECT version FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`,
			owner,
		); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	return nil
}
