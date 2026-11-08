//nolint:testpackage // Tests exercise the OS-held lock protocol across process death.
package descriptor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

func TestJournalLockChild(t *testing.T) {
	t.Parallel()
	path := os.Getenv("ZNS_CHATBOT_LOCK_TEST_PATH")
	if path == "" {
		return
	}
	lock, err := journalLock(path)
	require.NoError(t, err)
	defer lock.Close()
	if procedure := os.Getenv("ZNS_CHATBOT_LOCK_TEST_PROCEDURE"); procedure != "" {
		owned, lockErr := journalLock(procedure)
		require.NoError(t, lockErr)
		defer owned.Close()
	}
	fmt.Fprintln(os.Stdout, "journal locked")
	time.Sleep(time.Minute)
}

func TestJournalLockCrashReleasesSameInode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "worker.lock")
	procedure := filepath.Join(filepath.Dir(path), "procedure.lock")
	journal := filepath.Join(filepath.Dir(path), "journal.json")
	original := symboldescribe.State{Generation: uuid.New(), ProcedureID: uuid.New(),
		Reference: symbols.Reference{Kind: symbols.Sticker, ID: "original", FileID: "original-file"},
		Pending:   "upload_artifact", Phase: "uploading", ChatID: "original-chat"}
	require.NoError(t, save(journal, original))
	beforeJournal, err := os.ReadFile(journal)
	require.NoError(t, err)
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.CommandContext(t.Context(), executable, "-test.run=^TestJournalLockChild$")
	child.Env = append(os.Environ(), "ZNS_CHATBOT_LOCK_TEST_PATH="+path,
		"ZNS_CHATBOT_LOCK_TEST_PROCEDURE="+procedure)
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())
	t.Cleanup(func() { _ = child.Process.Kill() })
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		ready <- scanner.Scan() && scanner.Text() == "journal locked"
	}()
	select {
	case ok := <-ready:
		require.True(t, ok)
	case <-time.After(10 * time.Second):
		t.Fatal("child did not acquire journal ownership")
	}
	before, err := os.Stat(path)
	require.NoError(t, err)
	_, err = journalLock(path)
	require.Error(t, err, "another process must not acquire the held inode")
	_, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.ErrorIs(t, err, os.ErrExist, "legacy binary must not acquire a new-format lock")
	require.NoError(t, child.Process.Kill())
	require.Error(t, child.Wait())
	lock, err := journalLock(path)
	require.NoError(t, err, "process death must release ownership without deleting the inode")
	require.NoError(t, lock.Close())
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	owned, err := journalLock(procedure)
	require.NoError(t, err, "process death must release procedure ownership too")
	require.NoError(t, owned.Close())
	state, err := descriptorState(filepath.Dir(journal))
	require.NoError(t, err)
	require.Equal(t, original, state)
	err = (symboldescribe.Flow{}).Step(t.Context(), loom.Auth{}, &state, func(symboldescribe.State) error {
		t.Fatal("uncertain upload must not write a new receipt or attempt another effect")
		return nil
	})
	require.ErrorContains(t, err, "unknown prior effect")
	afterJournal, err := os.ReadFile(journal)
	require.NoError(t, err)
	require.Equal(t, beforeJournal, afterJournal)
}

func TestJournalLockRejectsLegacyWithoutChangingIt(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"", "incomplete"} {
		path := filepath.Join(t.TempDir(), "procedure.lock")
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))
		_, err := journalLock(path)
		require.ErrorContains(t, err, "offline reconciliation")
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, content, string(raw))
	}
}
