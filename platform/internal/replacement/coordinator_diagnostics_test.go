package replacement_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

type diagnosticEngine struct {
	*fixture

	beforeStop func()
}

func TestReplacementReadinessDiagnosticRetainsIncompletePredicate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		healthReady    bool
		admissionReady bool
		stage          string
		predicate      string
	}{
		{"admission", true, false, "admission", "missing_startup"},
		{"health", false, true, "process", "app_health_not_ready"},
		{"both", false, false, "process", "app_health_not_ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			f, c := newFixture(t)
			c.Logger = slog.New(slog.NewJSONHandler(&output, nil))
			c.ReadyTimeout = 20 * time.Millisecond
			f.onStart = func() {
				f.names = nil
				if test.admissionReady {
					f.names = []string{"zns:" + installation + ":" + newLaunch + ":admit"}
				}
				if !test.healthReady {
					for i := range f.inventory {
						if f.inventory[i].Component == "app" {
							f.inventory[i].Health = "starting"
						}
					}
				}
			}
			require.ErrorIs(t, c.Run(t.Context()), replacement.ErrDeadline)
			require.Equal(t, replacement.StateStopped, f.ledger.State)
			require.NotContains(t, f.events, "save:running")
			var record map[string]any
			require.NoError(t, json.NewDecoder(bytes.NewReader(output.Bytes())).Decode(&record))
			require.Equal(t, test.stage, record["stage"])
			require.Equal(t, test.predicate, record["predicate"])
			require.Equal(t, "readiness", record["deadline_class"])
			require.Equal(t, "readiness_deadline", record["error_category"])
			require.Equal(t, float64(len(replacement.Components())), record["containers"])
			count := float64(0)
			if test.admissionReady {
				count = 1
			}
			require.Equal(t, count, record["sessions"])
			require.Equal(t, count, record["admissions"])
			require.GreaterOrEqual(t, record["elapsed_ms"].(float64), float64(20))
		})
	}
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
