package replacement_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

type diagnosticEngine struct {
	*fixture

	beforeStop func()
}

func (d diagnosticEngine) Stop(ctx context.Context, containers []replacement.Container) error {
	if d.beforeStop != nil {
		d.beforeStop()
	}
	return d.fixture.Stop(ctx, containers)
}

func TestReplacementDiagnosticPrecedesCleanupAndDoesNotExposeErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		stage     string
		predicate string
		mutate    func(*fixture)
	}{
		{"admission", "admission", "missing_running", func(f *fixture) { f.names = nil }},
		{"process", "process", "not_running", func(f *fixture) { f.inventory[0].Running = false }},
		{"sessions", "sessions", "operation", func(f *fixture) { f.sessionErr = errors.New("secret DSN and SQL") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			f, c := newFixture(t)
			c.Logger = slog.New(slog.NewJSONHandler(&output, nil))
			f.onStart = func() { f.names = []string{"zns:" + installation + ":" + newLaunch + ":admit"} }
			f.onRunning = func() { test.mutate(f) }
			stops := 0
			c.Engine = diagnosticEngine{fixture: f, beforeStop: func() {
				stops++
				if stops > 1 {
					require.Contains(t, output.String(), "replacement monitor failure")
				}
			}}
			require.Error(t, c.Run(t.Context()))
			require.NotContains(t, output.String(), "secret DSN and SQL")
			var records []map[string]any
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			for decoder.More() {
				var record map[string]any
				require.NoError(t, decoder.Decode(&record))
				records = append(records, record)
			}
			require.Len(t, records, 3)
			require.Equal(t, test.stage, records[0]["stage"])
			require.Equal(t, test.predicate, records[0]["predicate"])
			require.Equal(t, "replacement cleanup outcome", records[2]["msg"])
			require.GreaterOrEqual(t, records[0]["elapsed_ms"].(float64), float64(0))
		})
	}
}
