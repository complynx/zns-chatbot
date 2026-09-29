package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Profile scripts return only host effect receipts to later planning. Arbitrary
// worker output can echo private identity values, including through other reads.

func (b *Bot) scriptProfileEffects(ctx context.Context, owner string, id int64) (bool, bool, error) {
	var profile, language bool
	err := b.DB.QueryRow(ctx, `WITH runs AS (
 SELECT run FROM bot.interactions i, jsonb_array_elements(i.content) run
 WHERE i.owner=$1 AND i.update_id=$2 AND i.kind='script_runs'
 ), calls AS (
 SELECT call FROM runs, jsonb_array_elements(COALESCE(NULLIF(run->'calls','null'::jsonb),'[]'::jsonb)) call
 ) SELECT
 EXISTS(SELECT 1 FROM runs WHERE run->>'private_profile'='true') OR EXISTS(SELECT 1 FROM calls WHERE call ? 'profile'),
 EXISTS(SELECT 1 FROM calls WHERE call ? 'language')`, owner, id).Scan(&profile, &language)
	return profile, language, err
}

func (b *Bot) refreshScriptProfileEffects(ctx context.Context, in incoming, id int64) error {
	profileChanged, languageChanged, err := b.scriptProfileEffects(ctx, in.owner, id)
	if err != nil {
		return err
	}
	if languageChanged {
		languageInput := in
		languageInput.text = "/language"
		return b.handleLanguage(ctx, languageInput, telegram.Update{ID: id})
	}
	if profileChanged {
		return b.refreshOpenProfile(ctx, in.owner, in.chat)
	}
	return nil
}

// Even a rejected private-field proposal must not retain its source or echo.
