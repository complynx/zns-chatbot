package bot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// The saved host plan binds each derived reply to its history generation.
// Check after reading a reply, including explicit renders after the update ended.
// Manual replies have no plan; fixed host system notices do not use history text.
func (b *Bot) historyReplyVisible(ctx context.Context, owner string, updateID int64) (bool, error) {
	var plan cachedPlan
	err := b.DB.QueryRow(ctx, `SELECT plan FROM bot.replies WHERE update_id=$1`, updateID).Scan(&plan)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if plan.SystemNotice != "" {
		return true, nil
	}
	err = b.validateHistoryPlan(ctx, owner, updateID, plan)
	if errors.Is(err, errHistoryPlanTerminal) {
		return false, nil
	}
	return err == nil, err
}
