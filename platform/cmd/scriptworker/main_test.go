package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestProcessDeadlineIncludesBlockedInput(t *testing.T) {
	if os.Getenv("ZNS_SCRIPT_TEST_CHILD") == "1" {
		runWorker(false)
		os.Exit(0)
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 3*scriptworker.ProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessDeadlineIncludesBlockedInput$")
	command.Env = append(os.Environ(), "ZNS_SCRIPT_TEST_CHILD=1")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	input, err := command.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	started := time.Now()
	err = command.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 124, exit.ExitCode())
	assert.GreaterOrEqual(t, time.Since(started), scriptworker.ProcessTimeout)
	assert.NoError(t, ctx.Err())
}
