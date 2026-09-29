package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestExportCommand(t *testing.T) {
	t.Parallel()
	var log, output, manifest bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	recorder.EmitCode(t.Context(), observability.AgentEvent{Phase: "script", Operation: "js.run", Outcome: "ok"},
		`return input.filter(item => item.value === "private-canary");`)
	path := filepath.Join(t.TempDir(), "input.jsonl")
	require.NoError(t, os.WriteFile(path, log.Bytes(), 0o600))
	require.NoError(t, run([]string{"-file", path, "-limit", "10"}, &output, &manifest))
	require.Contains(t, output.String(), "source_redacted")
	require.NotContains(t, output.String(), "private-canary")
	require.Contains(t, manifest.String(), `"records":1`)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, log.Bytes(), after)
}

func TestCommandErrorsDoNotEchoArguments(t *testing.T) {
	t.Parallel()
	var output, manifest bytes.Buffer
	err := run([]string{"-file", "private-canary", "-since", "secret-canary"}, &output, &manifest)
	require.EqualError(t, err, "invalid since timestamp")
	require.Empty(t, output.String())
	err = run([]string{"-file", "private-canary"}, &output, &manifest)
	require.EqualError(t, err, "cannot open diagnostic log")
}
