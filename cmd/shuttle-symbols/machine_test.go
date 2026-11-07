package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

func TestDescriptorRetriesMachineIssuerOutage(t *testing.T) {
	t.Parallel()
	require.True(t, transientDescriptorRead(identity.ErrUnavailable))
	require.False(t, transientDescriptorRead(identity.ErrDenied))
}

func TestFreshDescriptorJournalSurvivesMachineOutage(t *testing.T) {
	t.Parallel()
	a := arguments{journal: t.TempDir()}
	entry := db.SymbolRecord{Generation: uuid.New(), Reference: symbols.Reference{Kind: symbols.Sticker, ID: "fresh"}}
	directory := filepath.Join(a.journal, entry.Generation.String())
	var original uuid.UUID
	calls := 0
	step := func(_ context.Context, next arguments) error {
		calls++
		path := filepath.Join(next.journal, "journal.json")
		state, err := readJournal(next, path, entry, nil)
		require.NoError(t, err)
		if calls == 1 {
			original = state.ProcedureID
			return identity.ErrUnavailable
		}
		require.Equal(t, original, state.ProcedureID)
		require.Equal(t, entry.Generation, state.Generation)
		state.Phase = descriptorDonePhase
		return save(path, state)
	}
	done, err := advanceEntry(t.Context(), a, entry, step)
	require.NoError(t, err)
	require.False(t, done)
	_, err = os.Stat(filepath.Join(directory, "blocked.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
	retry, err := readDescriptorRetry(directory)
	require.NoError(t, err)
	_, err = advanceEntry(t.Context(), a, entry, step)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	retry.After = time.Now().Add(-time.Second)
	require.NoError(t, saveDescriptorRetry(directory, retry))
	done, err = advanceEntry(t.Context(), a, entry, step)
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, 2, calls)
}
