package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

// The ordinal is reserved durably before the script call, across all scripts in
// one update. Order versions cannot order hints belonging to different events.
func (b *Bot) storeFoodPending(
	ctx context.Context,
	owner string,
	update int64,
	command legacyfood.Command,
	sequence int,
) error {
	pending := struct {
		legacyfood.Command

		Sequence int `json:"preparation_order"`
	}{command, sequence}
	_, err := b.DB.Exec(ctx, `INSERT INTO bot.food_pending(owner,command,update_id) VALUES($1,$2,$3)
 ON CONFLICT(owner) DO UPDATE SET command=EXCLUDED.command,update_id=EXCLUDED.update_id,expires_at=now()+interval '24 hours'
 WHERE bot.food_pending.update_id<EXCLUDED.update_id OR
 (bot.food_pending.update_id=EXCLUDED.update_id
 AND COALESCE((bot.food_pending.command->>'preparation_order')::bigint,0)
 <(EXCLUDED.command->>'preparation_order')::bigint)`, owner, pending, update)
	return err
}
