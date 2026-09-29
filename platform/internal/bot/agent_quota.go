package bot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

// reserveAgentQuestion commits before model work. Failed attempts retain their
// reservation; replaying the trusted owner/update never spends another question.
func (b *Bot) reserveAgentQuestion(ctx context.Context, owner string, updateID int64) (bool, int, error) {
	var creditMode bool
	if err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.budget_operations WHERE owner=$1 AND update_id=$2 AND mode='credits')`, owner, updateID).
		Scan(&creditMode); err != nil {
		return false, 0, err
	}
	if creditMode {
		return true, -1, nil
	}
	limit := b.AssistantDailyLimit
	// Directly constructed bots use the same default as validated runtime config.
	if limit == 0 {
		limit = config.DefaultAssistantDailyLimit
	}
	if limit < 1 || limit > config.MaxAssistantDailyLimit {
		return false, 0, errors.New("invalid assistant quota")
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize only this owner's quota decisions, across all bot processes.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 39001))`, owner); err != nil {
		return false, 0, err
	}
	var allowed bool
	var remaining int
	err = tx.QueryRow(ctx, `SELECT allowed,remaining FROM bot.agent_quota WHERE owner=$1 AND update_id=$2`, owner, updateID).
		Scan(&allowed, &remaining)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `WITH usage AS (
		 SELECT count(*) AS used FROM bot.agent_quota
		 WHERE owner=$1 AND allowed AND reserved_at >= clock_timestamp()-interval '24 hours'
		) INSERT INTO bot.agent_quota(owner,update_id,allowed,remaining)
		 SELECT $1,$2,used<$3,GREATEST($3-used-1,0) FROM usage RETURNING allowed,remaining`, owner, updateID, limit).
			Scan(&allowed, &remaining)
	}
	if err != nil {
		return false, 0, err
	}
	return allowed, remaining, tx.Commit(ctx)
}

func (b *Bot) createPlan(ctx context.Context, in incoming, updateID int64) (cachedPlan, error) {
	allowed, remaining, err := b.reserveAgentQuestion(ctx, in.owner, updateID)
	if err != nil {
		return cachedPlan{}, err
	}
	if !allowed {
		return b.agentQuotaNotice(ctx, in.owner)
	}
	return b.createAllowedPlan(ctx, in, updateID, remaining)
}

func (b *Bot) agentQuotaNotice(ctx context.Context, owner string) (cachedPlan, error) {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return cachedPlan{}, err
	}
	text, err := i18n.Translate(preference.Language, i18n.AgentQuotaReached, nil)
	return cachedPlan{Plan: agent.Plan{Text: text}, SystemNotice: i18n.AgentQuotaReached}, err
}
