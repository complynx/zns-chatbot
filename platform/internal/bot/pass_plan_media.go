package bot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const mediaModelReplyKind = "media_model_reply"

// Save the text and its producing plan reference together; a delivery retry
// must not authorize old text against a newer unrelated plan.
func (b *Bot) storeMediaModelReply(
	ctx context.Context,
	owner, id, status, notice, text, action string,
	planUpdate int64,
) error {
	if err := b.planAuthorization().ValidateReply(ctx, owner, planUpdate); err != nil {
		return err
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var mediaUpdate int64
	err = tx.QueryRow(ctx, `UPDATE bot.media_intake SET status=$3,notice=$4,model_text=$5,last_action=$6,last_origin='agent'
 WHERE owner=$1 AND id=$2 AND status<>'done' AND command IS NULL AND registration_command IS NULL AND food_command IS NULL
 RETURNING update_id`, owner, id, status, notice, text, action).
		Scan(&mediaUpdate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, mediaUpdate, mediaModelReplyKind, planUpdate)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (b *Bot) passMediaTextVisible(ctx context.Context, owner, id string) (bool, error) {
	var planUpdate int64
	err := b.DB.QueryRow(ctx, `SELECT (r.content#>>'{}')::bigint FROM bot.media_intake m
 JOIN bot.interactions r ON r.owner=m.owner AND r.update_id=m.update_id AND r.kind=$3
 WHERE m.owner=$1 AND m.id=$2`, owner, id, mediaModelReplyKind).Scan(&planUpdate)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return b.planAuthorization().ReplyVisible(ctx, owner, planUpdate)
}

func (b *Bot) mediaModelQuestion(ctx context.Context, owner, id, question string, item mediaIntake) (string, error) {
	if item.Text == "" || item.Status == mediaDone {
		return question, nil
	}
	visible, err := b.passMediaTextVisible(ctx, owner, id)
	if err != nil || !visible {
		return question, err
	}
	return question + "\n" + item.Text, nil
}
