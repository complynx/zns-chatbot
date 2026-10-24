package integration_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

// Use the deployed process boundary: the HTTP server is race-instrumented, but
// rendering runs in its own bounded worker with the production deadline.
func avatarExecutable(t *testing.T) *avatar.Executable {
	t.Helper()
	name := "zinusya-avatar"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(
		t.Context(), "go", "build", "-race=false", "-mod=readonly", "-buildvcs=false", "-o", path,
		"../cmd/zinusya-avatar",
	)
	build.Env = append(build.Environ(), "CGO_ENABLED=0")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "Build avatar worker: %s", output)
	processor, err := avatar.NewExecutable(path)
	require.NoError(t, err)
	return processor
}
