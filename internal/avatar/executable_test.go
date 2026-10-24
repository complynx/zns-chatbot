package avatar_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

func testExecutable(t *testing.T, body string) *avatar.Executable {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	path := filepath.Join(t.TempDir(), "processor")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700))
	processor, err := avatar.NewExecutable(path)
	require.NoError(t, err)
	return processor
}

func posixTool(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixture")
	}
	tool, err := exec.LookPath(name)
	require.NoError(t, err)
	return tool
}

func TestExecutableCancellationKillsChild(t *testing.T) {
	t.Parallel()
	sleep := posixTool(t, "sleep")
	processor := testExecutable(t, "exec "+sleep+" 60")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := processor.Render(ctx, []byte("image"), avatar.IdentityTransform())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestExecutableCapsChildOutput(t *testing.T) {
	t.Parallel()
	head := posixTool(t, "head")
	processor := testExecutable(t, "exec "+head+" -c 7000000 /dev/zero")
	_, err := processor.Render(context.Background(), []byte("image"), avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrImageLimit)
}

func TestExecutableDoesNotInheritSecrets(t *testing.T) {
	t.Setenv("AVATAR_TEST_SECRET", "private")
	processor := testExecutable(t, "[ -z \"$AVATAR_TEST_SECRET\" ] || exit 2\nexit 3")
	_, err := processor.Render(context.Background(), []byte("image"), avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrImageLimit)
}

func TestExecutableRejectsOversizeInputBeforeStarting(t *testing.T) {
	t.Parallel()
	processor := testExecutable(t, "exit 2")
	_, err := processor.Render(context.Background(), make([]byte, avatar.MaxInputBytes+1), avatar.IdentityTransform())
	require.ErrorIs(t, err, avatar.ErrImageLimit)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = processor.Render(ctx, []byte("image"), avatar.IdentityTransform())
	require.ErrorIs(t, err, context.Canceled)
}
