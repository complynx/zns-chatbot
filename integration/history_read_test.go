package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func TestHistoricalOwnerReadsAreBoundedAndFullTextSurvives(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	strangerID, _ := registrationUser(t, h, 43, false)
	scope := historyScope(t, h, id)
	stranger := historyScope(t, h, strangerID)
	text := strings.Repeat("\t🙂\"Русский\n", 12000)
	collections := map[string][]map[string]any{
		"users":    {historyUser(42)},
		"messages": {historyMessage("long", text, 42)},
	}
	for range 23 {
		collections["messages"] = append(
			collections["messages"],
			historyMessage(uuid.NewString(), "Other historical message", 42),
		)
	}
	unsupported := historyMessage("system", "Never fresh instructions", 42)
	unsupported["role"] = "system"
	collections["messages"] = append(collections["messages"], unsupported, historyMessage("nul", "bad\x00text", 42))
	raw, digest := historyBundle(t, "read-test", collections)
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	ctx := context.Background()
	receipt, err := h.store.ApplyHistory(
		ctx,
		scope,
		prepareHistory(t, h.store, scope, a),
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.EqualValues(t, 27, receipt.Records)
	require.EqualValues(t, 25, receipt.Projected)
	seen := map[uuid.UUID]bool{}
	after := uuid.Nil
	longID := uuid.Nil
	for {
		page, pageErr := h.store.OwnerHistory(ctx, scope, after, 7)
		require.NoError(t, pageErr)
		require.LessOrEqual(t, len(page.Items), 7)
		for _, item := range page.Items {
			require.False(t, seen[item.ID])
			seen[item.ID] = true
			var preview struct {
				Content   string `json:"content"`
				Truncated bool   `json:"truncated"`
			}
			require.NoError(t, json.Unmarshal(item.Preview, &preview))
			if preview.Truncated {
				longID = item.ID
				require.Equal(t, 256, utf8.RuneCountInString(preview.Content))
				require.Contains(t, string(item.Preview), "2025-03-30T01:30:00+03:00")
			}
		}
		if !page.HasMore {
			break
		}
		require.NotEqual(t, after, page.NextID)
		after = page.NextID
	}
	require.Len(t, seen, 25)
	require.NotEqual(t, uuid.Nil, longID)
	var restored strings.Builder
	var start int32
	for {
		part, readErr := h.store.OwnerHistoryText(ctx, scope, longID, start)
		require.NoError(t, readErr)
		require.LessOrEqual(t, utf8.RuneCountInString(part.Content), 4096)
		encoded, marshalErr := json.Marshal(part)
		require.NoError(t, marshalErr)
		require.Less(t, len(encoded), 32<<10)
		restored.WriteString(part.Content)
		if !part.HasMore {
			break
		}
		require.Greater(t, part.NextRune, start)
		start = part.NextRune
	}
	require.Equal(t, text, restored.String())
	page, err := h.store.OwnerHistory(ctx, stranger, uuid.Nil, 0)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	_, err = h.store.OwnerHistoryText(ctx, stranger, longID, 0)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.OwnerHistoryText(ctx, scope, longID, int32(utf8.RuneCountInString(text))+1)
	require.ErrorIs(t, err, db.ErrInvalidHistory)
	_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", id)
	require.NoError(t, err)
	_, err = h.store.OwnerHistory(ctx, scope, uuid.Nil, 10)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestHistoricalImportRejectsCorruptStoredFooterAndExpiredProposal(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	collections := map[string][]map[string]any{"users": {historyUser(42)}}
	for range 210 {
		collections["messages"] = append(
			collections["messages"],
			historyMessage(uuid.NewString(), "Uncommitted message", 42),
		)
	}
	raw, digest := historyBundle(t, "late-corruption", collections)
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	r := prepareHistory(t, h.store, scope, a)
	ctx := context.Background()
	_, err := h.store.Pool().
		Exec(ctx, "UPDATE shuttle_legacy_archive_chunks SET content=set_byte(content,octet_" +
"length(content)-8,get_byte(content,octet_length(content)-8)#1)\nWHERE arc" +
"hive_id=$1 AND ordinal=(SELECT max(ordinal) FROM shuttle_legacy_archive_" +
"chunks WHERE archive_id=$1)", a.ArchiveID)
	require.NoError(t, err)
	_, err = h.store.ApplyHistory(ctx, scope, r, func(context.Context) error { return nil })
	require.ErrorIs(t, err, legacyimport.ErrInvalidBundle)
	var records, operations int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(ctx, "SELECT (SELECT count(*) FROM shuttle_legacy_history_records),(SELECT cou" +
"nt(*) FROM shuttle_legacy_history_operations)").
			Scan(&records, &operations),
	)
	require.Zero(t, records)
	require.Zero(t, operations)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_legacy_history_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour' WHERE id=$1", r.ProposalID)
	require.NoError(t, err)
	_, err = h.store.ApplyHistory(ctx, scope, r, func(context.Context) error { return nil })
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestHistoryPageByteBudgetAndLongDateAreExplicit(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	message := historyMessage("long-date", "Dated text", 42)
	message["date"] = map[string]any{"$date": "2025-03-30T01:30:00." + strings.Repeat("1", 600) + "+03:00"}
	collections := map[string][]map[string]any{"users": {historyUser(42)}, "messages": {message}}
	for range 24 {
		collections["passes"] = append(
			collections["passes"],
			map[string]any{
				"_id":     uuid.NewString(),
				"bot_id":  123,
				"user_id": 42,
				"pass_key": strings.Repeat(
					"\x01",
					256,
				),
				"state": strings.Repeat("\x01", 256),
				"type":  strings.Repeat("\x01", 256),
				"role":  strings.Repeat("\x01", 256),
			},
		)
	}
	raw, digest := historyBundle(t, "page-budget", collections)
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	ctx := context.Background()
	_, err := h.store.ApplyHistory(
		ctx,
		scope,
		prepareHistory(t, h.store, scope, a),
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	after := uuid.Nil
	seen := map[uuid.UUID]bool{}
	foundDate := false
	for {
		page, readErr := h.store.OwnerHistory(ctx, scope, after, 20)
		require.NoError(t, readErr)
		var encodedBytes int
		for _, item := range page.Items {
			encoded, marshalErr := json.Marshal(item)
			require.NoError(t, marshalErr)
			encodedBytes += len(encoded)
			require.False(t, seen[item.ID])
			seen[item.ID] = true
			if item.SourceRole == "messages" {
				foundDate = true
				require.Contains(t, string(item.Preview), `"recorded_date_omitted": true`)
				require.Contains(t, string(item.Preview), `"recorded_date": null`)
			}
		}
		require.LessOrEqual(t, encodedBytes, 32<<10)
		if !page.HasMore {
			break
		}
		require.Less(t, len(page.Items), 20)
		require.NotEqual(t, after, page.NextID)
		after = page.NextID
	}
	require.Len(t, seen, 26)
	require.True(t, foundDate)
}
