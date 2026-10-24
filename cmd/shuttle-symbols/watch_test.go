package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/symbols"
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
