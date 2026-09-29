package replacement_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

func TestJournalPreservesStateAndRejectsCorruption(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	journal := replacement.FileJournal{Directory: directory}
	empty, err := journal.Load()
	require.NoError(t, err)
	require.Zero(t, empty.Version)
	ledger := replacement.Ledger{
		Version:      1,
		Installation: installation,
		Host:         "host",
		Daemon:       "daemon",
		State:        replacement.StateStopping,
	}
	require.NoError(t, journal.Save(ledger))
	got, err := journal.Load()
	require.NoError(t, err)
	require.Equal(t, ledger, got)
	ledger.State = replacement.StateStopped
	require.NoError(t, journal.Save(ledger))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "ledger.json"), []byte("{partial"), 0600))
	_, err = journal.Load()
	require.ErrorIs(t, err, replacement.ErrUnknown)
}

func TestOwnerLockExcludesSecondLauncherAndRecoversAfterClose(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("deployment lock requires Linux")
	}
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0700))
	unlock, err := replacement.Lock(directory)
	require.NoError(t, err)
	other, err := replacement.Lock(directory)
	require.ErrorIs(t, err, replacement.ErrUnknown)
	require.Nil(t, other)
	require.NoError(t, unlock())
	recovered, err := replacement.Lock(directory)
	require.NoError(t, err)
	require.NoError(t, recovered())
}
