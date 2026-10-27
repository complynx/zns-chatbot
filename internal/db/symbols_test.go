package db_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

func TestSymbolDescriptionWaitUsesOriginalScopedIntakeDeadline(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 1)
	open, err := s.SymbolDescriptionWaitOpen(t.Context(), 1, task.ID)
	require.NoError(t, err)
	require.True(t, open)
	_, err = s.SymbolDescriptionWaitOpen(t.Context(), 2, task.ID)
	require.Error(t, err)
	_, err = s.Pool().Exec(t.Context(),
		"UPDATE shuttle_tasks SET created_at=now()-interval '7 minutes' WHERE id=$1", task.ID)
	require.NoError(t, err)
	open, err = s.SymbolDescriptionWaitOpen(t.Context(), 1, task.ID)
	require.NoError(t, err)
	require.False(t, open)
}

func TestPendingSymbolQueueIsScopedAndDoesNotRegenerateCompletedEntries(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	for update, key := range []string{"pending", "done"} {
		ref := symbols.Reference{Kind: symbols.Sticker, ID: key}
		_, _, err := s.Intake(ctx, db.IntakeRequest{BotID: 1, UpdateID: int64(update + 1), TelegramUserID: 7,
			ChatID: 7, Payload: json.RawMessage(`{}`),
			Symbols: []symbols.Occurrence{{Reference: ref}}}, nil)
		require.NoError(t, err)
	}
	entry, err := s.PendingSymbol(ctx, 1, symbols.Sticker, "done")
	require.NoError(t, err)
	require.NoError(t, s.CompleteSymbolDescription(ctx, 1, entry,
		symbols.Description{Text: "A smiling cat", Category: "emotion"}, entry.Workflow))
	queued, err := s.PendingSymbols(ctx, 1, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.Equal(t, "pending", queued[0].Reference.ID)
	tail, err := s.PendingSymbols(ctx, 1, queued[0].Generation)
	require.NoError(t, err)
	require.Empty(t, tail)
	foreign, err := s.PendingSymbols(ctx, 2, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, foreign)
}

func TestSymbolsIntakeDedupDecayAndIndependentCaps(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	_, err := s.Pool().Exec(ctx, `UPDATE shuttle_settings SET document=document||
'{"sticker_catalog_limit":2,"emoji_catalog_limit":1,"symbol_half_life_seconds":60,
"symbol_min_half_life_seconds":60}'::jsonb`)
	require.NoError(t, err)
	accept := func(update int64, kind, key string) {
		ref := symbols.Reference{Kind: kind, ID: key}
		r := db.IntakeRequest{BotID: 1, UpdateID: update, TelegramUserID: 7, ChatID: 7, Payload: json.RawMessage(`{}`),
			Symbols: []symbols.Occurrence{{Reference: ref}}}
		_, _, err = s.Intake(ctx, r, nil)
		require.NoError(t, err)
	}
	accept(1, symbols.Sticker, "old")
	accept(1, symbols.Sticker, "old")
	var score float64
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT score FROM shuttle_symbols WHERE asset_key='old'").Scan(&score))
	require.InDelta(t, 1.0, score, 0.0001)
	_, err = s.Pool().Exec(ctx, "UPDATE shuttle_symbols SET score=8,last_used_at=now()-interval '120 seconds'")
	require.NoError(t, err)
	accept(2, symbols.Sticker, "old")
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT score FROM shuttle_symbols WHERE asset_key='old'").Scan(&score))
	require.InDelta(t, 3.0, score, 0.01)
	accept(3, symbols.Sticker, "new")
	accept(4, symbols.Sticker, "newest")
	var count int
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_symbols").Scan(&count))
	require.Equal(t, 2, count)
	accept(5, symbols.Emoji, "10")
	accept(6, symbols.Emoji, "11")
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_symbols").Scan(&count))
	require.Equal(t, 3, count)
	var retained bool
	require.NoError(
		t,
		s.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shuttle_symbols WHERE asset_key='old')").Scan(&retained),
	)
	require.True(t, retained)
}

func TestSymbolDescriptionGenerationAndScope(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	r := symbols.Reference{Kind: symbols.Emoji, ID: "10"}
	_, _, err := s.Intake(ctx, db.IntakeRequest{BotID: 1, UpdateID: 1, TelegramUserID: 7, ChatID: 7,
		Payload: json.RawMessage(`{}`), Symbols: []symbols.Occurrence{{Reference: r}}}, nil)
	require.NoError(t, err)
	entry, err := s.PendingSymbol(ctx, 1, r.Kind, r.ID)
	require.NoError(t, err)
	next := json.RawMessage(`{"phase":"running"}`)
	require.NoError(t, s.SaveSymbolWorkflow(ctx, 1, entry, entry.Workflow, next))
	require.ErrorIs(t, s.SaveSymbolWorkflow(ctx, 1, entry, entry.Workflow, next), db.ErrConflict)
	d := symbols.Description{Text: "Horizontal line", Category: "ornament"}
	require.NoError(t, s.CompleteSymbolDescription(ctx, 1, entry, d, next))
	require.NoError(t, s.CompleteSymbolDescription(ctx, 1, entry, d, next))
	completed, err := s.PendingSymbol(ctx, 1, r.Kind, r.ID)
	require.NoError(t, err)
	require.True(t, completed.Described)
	items := []symbols.Occurrence{{Reference: r}}
	descriptions, err := s.SymbolDescriptions(ctx, 1, items)
	require.NoError(t, err)
	require.Equal(t, d, descriptions["emoji:10"])
	descriptions, err = s.SymbolDescriptions(ctx, 2, items)
	require.NoError(t, err)
	require.Empty(t, descriptions)
}

func TestEvictedSymbolUsageCanPromoteNewcomer(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	_, err := s.Pool().Exec(ctx, `UPDATE shuttle_settings SET document=document||'{"emoji_catalog_limit":1}'::jsonb`)
	require.NoError(t, err)
	accept := func(update int64, id string) {
		ref := symbols.Reference{Kind: symbols.Emoji, ID: id}
		items := []symbols.Occurrence{{Reference: ref}}
		payload, marshalErr := json.Marshal(struct {
			Symbols []symbols.Occurrence `json:"symbols"`
		}{items})
		require.NoError(t, marshalErr)
		_, _, intakeErr := s.Intake(ctx, db.IntakeRequest{BotID: 1, UpdateID: update, TelegramUserID: 7, ChatID: 7,
			Payload: payload, Symbols: items}, nil)
		require.NoError(t, intakeErr)
	}
	for i := int64(1); i <= 5; i++ {
		accept(i, "1")
	}
	for i := int64(6); i <= 11; i++ {
		accept(i, "2")
	}
	var key string
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT asset_key FROM shuttle_symbols").Scan(&key))
	require.Equal(t, "2", key)
}
