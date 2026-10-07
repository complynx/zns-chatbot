package replacement

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDockerCommandReportsOnlyFixedStderrClassification(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, test := range []struct {
		name     string
		stderr   string
		exitCode int
		category string
	}{
		{"thread-denial", "runtime: failed to create new OS thread (have 12 already; errno=11)", 2, "thread_creation_denied"},
		{"large-thread-trace", "runtime: failed to create new OS thread\n" + strings.Repeat("x", commandStderrLimit*8), 2, "thread_creation_denied"},
		{"ordinary-error", "private configuration details", 2, "command_exit"},
		{"other-exit", "runtime: failed to create new OS thread", 1, "command_exit"},
		{"stream-cap", strings.Repeat("x", commandStderrLimit+1), 2, "command_exit"},
		{"outside-private-prefix", strings.Repeat("x", commandStderrLimit) + "runtime: failed to create new OS thread", 2, "command_exit"},
	} {
		script := "#!/bin/sh\nprintf '%s\\n' 'secret-canary' '" + test.stderr + "' >&2\nexit " + strconv.Itoa(
			test.exitCode,
		) + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0700), test.name)
		docker := Docker{Command: DockerCommand{}}
		data, err := docker.call(t.Context(), []string{"info"}, nil)
		require.Error(t, err)
		require.Empty(t, data)
		require.NotContains(t, err.Error(), "secret-canary")
		var output bytes.Buffer
		recordFailure(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)), err)
		require.NotContains(t, output.String(), "secret-canary")
		require.NotContains(t, output.String(), test.stderr)
		decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
		decoder.UseNumber()
		var record map[string]any
		require.NoError(t, decoder.Decode(&record))
		require.Equal(t, test.category, record["error_category"])
		require.Equal(t, json.Number(strconv.Itoa(test.exitCode)), record["command_exit_code"])
	}
}

func TestDockerCommandCancellationStillBoundsExecution(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "docker"), []byte("#!/bin/sh\nexec sleep 10\n"), 0700))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	docker := Docker{Command: DockerCommand{}}
	_, err := docker.call(ctx, []string{"info"}, nil)
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
	var output bytes.Buffer
	recordFailure(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)), err)
	require.Contains(t, output.String(), "deadline_exceeded")
	require.NotContains(t, output.String(), "thread_creation_denied")
}

func TestDockerCommandStdoutCapRemainsEnforced(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\nexec head -c " + strconv.Itoa(commandOutputLimit+1) + " /dev/zero\n"
	require.NoError(t, os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0700))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	docker := Docker{Command: DockerCommand{}}
	data, err := docker.call(ctx, []string{"info"}, nil)
	require.Error(t, err)
	require.Empty(t, data)
	require.NoError(t, ctx.Err(), "stdout must fail at its cap, not the deadline")
}
