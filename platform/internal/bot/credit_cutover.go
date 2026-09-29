package bot

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

const budgetLegacy = "legacy"

// bindBudget runs before media/provider work. A host update retains its budget
// mode across retries and across the explicit deployment cutover epoch.
func (b *Bot) bindBudget(ctx context.Context, owner string, update int64) (context.Context, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return ctx, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,39001))`, owner); err != nil {
		return ctx, err
	}
	var epoch *time.Time
	if err = tx.QueryRow(ctx, `SELECT (SELECT epoch FROM credits.cutover WHERE singleton)`).Scan(&epoch); err != nil {
		return ctx, err
	}
	var mode string
	err = tx.QueryRow(ctx, `SELECT mode FROM bot.budget_operations WHERE owner=$1 AND update_id=$2`, owner, update).
		Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		mode = budgetLegacy
		var existing bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.agent_quota WHERE owner=$1 AND update_id=$2)`, owner, update).
			Scan(&existing); err != nil {
			return ctx, err
		}
		if epoch != nil && !existing {
			mode = "credits"
		}
		_, err = tx.Exec(
			ctx,
			`INSERT INTO bot.budget_operations(owner,update_id,mode,cutover_epoch) VALUES($1,$2,$3,$4)`,
			owner,
			update,
			mode,
			epoch,
		)
	}
	if err != nil {
		return ctx, err
	}
	if err = b.creditCutoverConfiguration(epoch != nil); err != nil {
		return ctx, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ctx, err
	}
	scope := credits.ScopeFromContext(ctx)
	scope.LegacyBudget = mode == budgetLegacy
	return credits.WithScope(ctx, scope), nil
}
