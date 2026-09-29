package agenthost

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/account"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

// QuestionQuota owns durable question admission; failed attempts retain their slot.
type QuestionQuota struct {
	DB          *pgxpool.Pool
	Limit       int
	Preferences interface {
		Preferences(context.Context, string) (account.Preferences, error)
	}
}

// ReserveQuestion commits before model work. Failed attempts retain their
// reservation; replaying the trusted owner/update never spends another question.
func (q QuestionQuota) ReserveQuestion(ctx context.Context, owner string, updateID int64) (bool, int, error) {
	var creditMode bool
	if err := q.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.budget_operations WHERE owner=$1 AND update_id=$2 AND mode='credits')`, owner, updateID).
		Scan(&creditMode); err != nil {
		return false, 0, err
	}
	if creditMode {
		return true, -1, nil
	}
	limit := q.Limit
	// Directly constructed hosts use the same default as validated runtime config.
	if limit == 0 {
		limit = config.DefaultAssistantDailyLimit
	}
	if limit < 1 || limit > config.MaxAssistantDailyLimit {
		return false, 0, errors.New("invalid assistant quota")
	}
	tx, err := q.DB.Begin(ctx)
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

func (q QuestionQuota) QuotaNotice(ctx context.Context, owner string) (interaction.SavedPlan, error) {
	preference, err := q.Preferences.Preferences(ctx, owner)
	if err != nil {
		return interaction.SavedPlan{}, err
	}
	text, err := i18n.Translate(preference.Language, i18n.AgentQuotaReached, nil)
	return interaction.SavedPlan{Plan: agent.Plan{Text: text}, SystemNotice: i18n.AgentQuotaReached}, err
}
