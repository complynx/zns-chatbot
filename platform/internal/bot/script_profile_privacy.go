package bot

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func scriptHasProfileWrite(record scriptRecord) bool {
	if record.PrivateProfile {
		return true
	}
	for _, call := range record.Calls {
		if call.Profile != nil {
			return true
		}
	}
	return false
}

// Profile scripts return only host effect receipts to later planning. Arbitrary
// worker output can echo private identity values, including through other reads.
func redactProfileScript(record *scriptRecord) {
	if !scriptHasProfileWrite(*record) {
		return
	}
	record.Request.Code = ""
	record.Request.InputJSON = ""
	record.Run.Code = ""
	record.Run.Result = json.RawMessage(
		`{"private_profile_script":true,"evidence":"Inspect host call outcomes; read profile.get for current state."}`,
	)
	for i := range record.Calls {
		call := &record.Calls[i]
		if call.Profile != nil {
			metadata := *call.Profile
			metadata.Value = ""
			call.Profile = &metadata
		}
		if call.Profile == nil && call.Language == nil && call.Outcome.Error == "" {
			call.Outcome.Result = json.RawMessage(`{"completed":true,"private_payload_omitted":true}`)
		}
	}
}

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
func (b *Bot) markPrivateProfileScript(ctx context.Context, owner string, id int64, index int) error {
	return b.updateScriptTools(ctx, owner, id, index, func(_ pgx.Tx, records []scriptRecord) error {
		record := &records[index]
		record.PrivateProfile = true
		record.Request.Code = ""
		record.Request.InputJSON = ""
		record.Run.Code = ""
		return nil
	})
}
