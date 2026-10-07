package replacement

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

type failingStartupCommand struct{ failCreate bool }

func (command failingStartupCommand) Run(_ context.Context, args, _ []string) ([]byte, error) {
	if command.failCreate && args[len(args)-1] == "json" {
		return []byte(
			`{"services":{"app":{},"evaluator":{},"media-decoder":{},"media-broker":{},"sticker-decoder":{},"sticker-broker":{}}}`,
		), nil
	}
	return nil, &commandError{exitCode: 17}
}

func TestStartupCommandFailureReportsPhaseAndExitCode(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"config", "create"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			docker := Docker{
				Command:      failingStartupCommand{failCreate: operation == "create"},
				Installation: "010700000225", Project: "config", Files: []string{"/config/runtime.compose.yaml"},
			}
			_, err := docker.Create(t.Context(), runtimeapp.Instance{
				Installation: docker.Installation, Launch: "94d8c07929c04484cc9c285f",
			})
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
