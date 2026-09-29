package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/jackc/pgx/v5"
)

// Derived output must retain both its history generation and current read authority.
func (b *Bot) derivedReplyVisible(ctx context.Context, owner string, updateID int64) (bool, error) {
	origin, err := (interaction.Store{DB: b.DB}).ReplyOrigin(ctx, owner, updateID)
	if err == nil && origin == interaction.TrustedReply {
		return true, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	visible, err := b.historyReplyVisible(ctx, owner, updateID)
	if err != nil || !visible {
		return visible, err
	}
	return b.planAuthorization().ReplyVisible(ctx, owner, updateID)
}

// The saved host plan binds each derived reply to its history generation.
// Check after reading a reply, including explicit renders after the update ended.
// Trusted replies are handled by their explicit origin before this check.
func (b *Bot) historyReplyVisible(ctx context.Context, owner string, updateID int64) (bool, error) {
	plan, err := (interaction.Store{DB: b.DB}).Load(ctx, owner, updateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if plan.SystemNotice != "" {
		return true, nil
	}
	err = b.planAuthorization().ValidateHistoryPlan(ctx, owner, updateID, plan)
	if errors.Is(err, errHistoryPlanTerminal) {
		return false, nil
	}
	return err == nil, err
}
