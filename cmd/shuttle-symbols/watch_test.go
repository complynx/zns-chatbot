package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestQueuedGenerationCannotAdoptAnEntryReadmittedBeforeJournalCreation(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "original-generation")
	a := arguments{kind: symbols.Sticker, id: "same-id", journal: dir, generation: uuid.New()}
	entry := db.SymbolRecord{Generation: uuid.New(), Reference: symbols.Reference{Kind: a.kind, ID: a.id}}
	_, err := readJournal(a, filepath.Join(dir, "journal.json"), entry, nil)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBlockedJournalRetainsSafeCauseWithoutRawError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		cause   error
		kind    string
		status  int
		outcome string
	}{
		{"telegram", fmt.Errorf("read sticker set: %w", &telegram.Error{
			StatusCode: 403, Outcome: telegram.Rejected,
		}), "telegram", 403, "rejected"},
		{"loom", &loom.Error{StatusCode: 503, Outcome: loom.Unknown}, "loom", 503, "unknown"},
		{"raw secret", errors.New("GET https://example.test/botSECRET: failure"), "descriptor", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			require.NoError(t, markBlocked(directory, tc.cause))
			raw, err := os.ReadFile(filepath.Join(directory, "blocked.json"))
			require.NoError(t, err)
			require.NotContains(t, string(raw), "SECRET")
			var got blockedFailure
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, blockedFailure{true, tc.kind, tc.status, tc.outcome}, got)
		})
	}
}
