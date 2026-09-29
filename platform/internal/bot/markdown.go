package bot

import (
	"context"
	"encoding/json"
)

// Persist formatting provenance in the same insert as the reply. Existing and
// manual interaction rows remain literal; only non-action model answers opt in.
func (b *Bot) recordReply(ctx context.Context, owner string, id int64, kind string, content any, native bool) error {
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	// Reject stale archives before a reply can be selected for rendering.
	if err = b.archiveReply(ctx, owner, id, kind, content); err != nil {
		return err
	}
	_, err = b.DB.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content,native_markdown)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, owner, id, kind, raw, native)
	if err != nil {
		return err
	}
	return nil
}
