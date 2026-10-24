package db_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

func TestSymbolDecayUsesSeparateBoundedMedianPeriodically(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	_, err := s.Pool().Exec(ctx, `UPDATE shuttle_settings SET document=document||
'{"symbol_half_life_seconds":500,"symbol_min_half_life_seconds":60,
"symbol_max_half_life_seconds":1000,"symbol_median_multiplier":2}'::jsonb`)
	require.NoError(t, err)
	accept := func(update int64) {
		ref := symbols.Reference{Kind: symbols.Emoji, ID: strconv.FormatInt(update, 10)}
		items := []symbols.Occurrence{{Reference: ref}}
		raw, marshalErr := json.Marshal(struct {
			Symbols []symbols.Occurrence `json:"symbols"`
		}{items})
		require.NoError(t, marshalErr)
		_, _, intakeErr := s.Intake(ctx, db.IntakeRequest{BotID: 1, UpdateID: update, TelegramUserID: 7, ChatID: 7,
			Payload: raw, Symbols: items}, nil)
		require.NoError(t, intakeErr)
	}
	for n := int64(1); n <= 10; n++ {
		accept(n)
	}
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_symbols SET last_interval_seconds=100;
UPDATE shuttle_symbol_decay SET evaluated_at=now()-interval '2 hours';`)
	require.NoError(t, err)
	accept(11)
	var emojiHalf, stickerHalf int
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, "SELECT half_life_seconds FROM shuttle_symbol_decay WHERE kind='emoji'").Scan(&emojiHalf),
	)
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, "SELECT half_life_seconds FROM shuttle_symbol_decay WHERE kind='sticker'").Scan(&stickerHalf),
	)
	require.Equal(t, 200, emojiHalf)
	require.Equal(t, 500, stickerHalf)
	_, err = s.Pool().Exec(ctx, "UPDATE shuttle_symbols SET last_interval_seconds=100000")
	require.NoError(t, err)
	accept(12)
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, "SELECT half_life_seconds FROM shuttle_symbol_decay WHERE kind='emoji'").Scan(&emojiHalf),
	)
	require.Equal(t, 200, emojiHalf, "Fresh hourly policy must not jump with each message")
	_, err = s.Pool().Exec(ctx, "UPDATE shuttle_symbol_decay SET evaluated_at=now()-interval '2 hours'")
	require.NoError(t, err)
	accept(13)
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, "SELECT half_life_seconds FROM shuttle_symbol_decay WHERE kind='emoji'").Scan(&emojiHalf),
	)
	require.Equal(t, 1000, emojiHalf)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_settings SET document=document||
'{"symbol_max_half_life_seconds":600,"symbol_median_multiplier":0.5}'::jsonb`)
	require.NoError(t, err)
	accept(14)
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, "SELECT half_life_seconds FROM shuttle_symbol_decay WHERE kind='emoji'").Scan(&emojiHalf),
	)
	require.Equal(t, 600, emojiHalf, "Changed policy must invalidate a fresh cached half-life")
}

func TestSymbolDecayLongGapsNeverAbortIntake(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	accept := func(update int64, key string) {
		ref := symbols.Reference{Kind: symbols.Emoji, ID: key}
		items := []symbols.Occurrence{{Reference: ref}}
		raw, marshalErr := json.Marshal(struct {
			Symbols []symbols.Occurrence `json:"symbols"`
		}{items})
		require.NoError(t, marshalErr)
		_, _, intakeErr := s.Intake(ctx, db.IntakeRequest{BotID: 1, UpdateID: update, TelegramUserID: 7, ChatID: 7,
			Payload: raw, Symbols: items}, nil)
		require.NoError(t, intakeErr)
	}
	accept(1, "1")
	_, err := s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET created_at=now()-interval '10 years';
UPDATE shuttle_symbols SET last_used_at=now()-interval '10 years',score=1e-200;
UPDATE shuttle_settings SET document=document||'{"symbol_half_life_seconds":60,
"symbol_min_half_life_seconds":60,"symbol_max_half_life_seconds":1000}'::jsonb;`)
	require.NoError(t, err)
	accept(2, "1")
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET created_at=now()-interval '10 years';
DELETE FROM shuttle_symbols;`)
	require.NoError(t, err)
	accept(3, "1")
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_settings SET document=document||
'{"symbol_half_life_seconds":120}'::jsonb`)
	require.NoError(t, err)
	accept(4, "2")
	var score float64
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT shuttle_symbol_weight(1e-200,1000000,60)").Scan(&score))
	require.Zero(t, score)
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT score FROM shuttle_symbols WHERE asset_key='1'").Scan(&score))
	require.InDelta(t, 1.0, score, 0.01)
}
