package replacement

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingStartupCommand struct{}

func (failingStartupCommand) Run(context.Context, []string, []string) ([]byte, error) {
	return nil, &commandError{exitCode: 17}
}

func TestStartupCommandFailureReportsPhaseAndExitCode(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"config", "create"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			docker := Docker{Command: failingStartupCommand{}}
			_, err := docker.call(t.Context(), []string{"compose", operation}, nil)
			require.Error(t, err)
			var output bytes.Buffer
			recordFailure(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)), err)
			var record map[string]any
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&record))
			require.Equal(t, "compose_"+operation, record["stage"])
			require.Equal(t, "command_exit", record["error_category"])
			require.Equal(t, json.Number("17"), record["command_exit_code"])
		})
	}
}
