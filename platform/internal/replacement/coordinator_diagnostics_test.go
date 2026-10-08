package replacement_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

type diagnosticEngine struct {
	*fixture

	beforeStop func()
}

type failedStartupEngine struct{ *fixture }

func (failedStartupEngine) Create(context.Context, runtimeapp.Instance) ([]replacement.Container, error) {
	return nil, errors.New("secret startup credentials")
}

func TestReplacementStartupFailureIsReportedWithoutSecrets(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	f, c := newFixture(t)
	c.Logger = slog.New(slog.NewJSONHandler(&output, nil))
	c.Engine = failedStartupEngine{fixture: f}
	require.Error(t, c.Run(t.Context()))
	require.Equal(t, replacement.StateStopped, f.ledger.State)
	require.NotContains(t, f.events, "save:running")
	require.NotContains(t, output.String(), "secret startup credentials")
	var record map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record))
	require.Equal(t, "launch", record["stage"])
	require.Equal(t, "operation_failed", record["error_category"])
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
				configureReadiness(f, test.healthReady, test.admissionReady)
			}
			require.ErrorIs(t, c.Run(t.Context()), replacement.ErrDeadline)
			require.Equal(t, replacement.StateStopped, f.ledger.State)
			require.NotContains(t, f.events, "save:running")
			var record map[string]any
			decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
			decoder.UseNumber()
			require.NoError(t, decoder.Decode(&record))
			require.Equal(t, test.stage, record["stage"])
			require.Equal(t, test.predicate, record["predicate"])
			require.Equal(t, "readiness", record["deadline_class"])
			require.Equal(t, "readiness_deadline", record["error_category"])
			require.Equal(t, json.Number(strconv.Itoa(len(replacement.Components()))), record["containers"])
			count := json.Number("0")
			if test.admissionReady {
				count = "1"
			}
			require.Equal(t, count, record["sessions"])
			require.Equal(t, count, record["admissions"])
			elapsed, err := record["elapsed_ms"].(json.Number).Int64()
			require.NoError(t, err)
			require.GreaterOrEqual(t, elapsed, int64(20))
		})
	}
}

func configureReadiness(f *fixture, healthReady, admissionReady bool) {
	f.names = nil
	if admissionReady {
		f.names = []string{"zns:" + installation + ":" + newLaunch + ":admit"}
	}
	if healthReady {
		return
	}
	for i := range f.inventory {
		if f.inventory[i].Component == "app" {
			f.inventory[i].Health = "starting"
		}
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
		{"identity", "docker_identity", "unknown_container", func(f *fixture) { f.inventory[0].Image = "unknown" }},
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
			decoder.UseNumber()
			for decoder.More() {
				var record map[string]any
				require.NoError(t, decoder.Decode(&record))
				records = append(records, record)
			}
			require.Len(t, records, 3)
			require.Equal(t, test.stage, records[0]["stage"])
			require.Equal(t, test.predicate, records[0]["predicate"])
			require.Equal(t, json.Number(strconv.Itoa(len(replacement.Components()))), records[0]["containers"])
			require.Equal(t, "replacement cleanup outcome", records[2]["msg"])
			elapsed, err := records[0]["elapsed_ms"].(json.Number).Int64()
			require.NoError(t, err)
			require.GreaterOrEqual(t, elapsed, int64(0))
		})
	}
}

func TestReplacementJournalDiagnosticRetainsReadyCounts(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	f, c := newFixture(t)
	c.Logger = slog.New(slog.NewJSONHandler(&output, nil))
	f.onStart = func() { configureReadiness(f, true, true); f.saveFailure = replacement.StateRunning }
	require.Error(t, c.Run(t.Context()))
	var record map[string]any
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&record))
	require.Equal(t, "journal", record["stage"])
	require.Equal(t, "save_running", record["predicate"])
	require.Equal(t, json.Number(strconv.Itoa(len(replacement.Components()))), record["containers"])
	require.Equal(t, json.Number("1"), record["sessions"])
	require.Equal(t, json.Number("1"), record["admissions"])
	require.Equal(t, replacement.StateStopped, f.ledger.State)
}

func TestReplacementFailureRetainsFirstProcessBeforeCleanup(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	f, c := newFixture(t)
	c.Logger = slog.New(slog.NewJSONHandler(&output, nil))
	f.onStart = func() { configureReadiness(f, true, true) }
	f.onRunning = func() {
		f.inventory[0].Running = false
		f.inventory[0].Status = "exited"
		f.inventory[0].ExitCode = 137
		f.inventory[0].OOMKilled = true
		f.inventory[1].Running = false
		f.inventory[1].Status = "exited"
		f.inventory[1].ExitCode = 1
	}
	c.Engine = diagnosticEngine{fixture: f, beforeStop: func() {
		if f.ledger.State == replacement.StateStopping && f.ledger.Launch == newLaunch {
			require.Contains(t, output.String(), `"id":"evaluator-new"`)
		}
	}}
	require.ErrorIs(t, c.Run(t.Context()), replacement.ErrStopped)
	require.Empty(t, f.inventory)
	require.Equal(t, replacement.StateStopped, f.ledger.State)
	var record map[string]any
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&record))
	require.Equal(t, "process", record["stage"])
	require.Equal(t, "not_running", record["predicate"])
	require.Equal(t, map[string]any{
		"component": "evaluator", "id": "evaluator-new", "status": "exited",
		"running": false, "exit_code": json.Number("137"), "oom_killed": true,
	}, record["container"])
}

func TestDiagnosticProcessStatePreservesVersionOneJournalReaders(t *testing.T) {
	t.Parallel()
	// This is the deployed version1 reader schema, before process diagnostics.
	type oldContainer struct {
		ID         string `json:"id"`
		Component  string `json:"component"`
		Launch     string `json:"launch"`
		Image      string `json:"image"`
		Created    string `json:"created"`
		Running    bool   `json:"running"`
		Restarting bool   `json:"restarting"`
		Paused     bool   `json:"paused"`
		PID        int    `json:"pid"`
		Health     string `json:"health"`
	}
	type oldLedger struct {
		Version      int            `json:"version"`
		Installation string         `json:"installation"`
		Host         string         `json:"host"`
		Daemon       string         `json:"daemon"`
		Generation   uint64         `json:"generation"`
		Launch       string         `json:"launch"`
		State        string         `json:"state"`
		Containers   []oldContainer `json:"containers"`
	}
	directory := t.TempDir()
	journal := replacement.FileJournal{Directory: directory}
	ledger := replacement.Ledger{
		Version: 1, Installation: installation, Host: "host", Daemon: "daemon",
		Generation: 2, Launch: newLaunch, State: replacement.StateStopping,
		Containers: []replacement.Container{{ID: "failed-current", Component: "evaluator", Launch: newLaunch,
			Image: "pinned-image", Created: "2026-10-08T18:16:19Z", Status: "exited", ExitCode: 137, OOMKilled: true}},
	}
	require.NoError(t, journal.Save(ledger))
	data, err := os.ReadFile(filepath.Join(directory, "ledger.json"))
	require.NoError(t, err)
	var prior oldLedger
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&prior))
	require.Equal(t, 1, prior.Version)
	require.Equal(t, newLaunch, prior.Launch)
	require.Equal(t, []oldContainer{{ID: "failed-current", Component: "evaluator", Launch: newLaunch,
		Image: "pinned-image", Created: "2026-10-08T18:16:19Z"}}, prior.Containers)
	got, err := journal.Load()
	require.NoError(t, err)
	ledger.Containers[0].Status = ""
	ledger.Containers[0].ExitCode = 0
	ledger.Containers[0].OOMKilled = false
	require.Equal(t, ledger, got)
	require.NoError(t, journal.Save(got))
	roundtrip, err := os.ReadFile(filepath.Join(directory, "ledger.json"))
	require.NoError(t, err)
	require.JSONEq(t, string(data), string(roundtrip))
}

type retirementObservationEngine struct {
	*fixture

	stopped          bool
	inventoryFailure bool
	waitForDeadline  bool
}

func (e *retirementObservationEngine) Stop(ctx context.Context, containers []replacement.Container) error {
	err := e.fixture.Stop(ctx, containers)
	e.stopped = true
	return err
}

func (e *retirementObservationEngine) Inventory(ctx context.Context) ([]replacement.Container, error) {
	if e.stopped && e.inventoryFailure {
		if e.waitForDeadline {
			<-ctx.Done()
		}
		return nil, replacement.ErrUnknown
	}
	return e.fixture.Inventory(ctx)
}

func (e *retirementObservationEngine) Names(ctx context.Context) ([]string, error) {
	if e.stopped && !e.inventoryFailure {
		if e.waitForDeadline {
			<-ctx.Done()
		}
		return nil, replacement.ErrUnknown
	}
	return e.fixture.Names(ctx)
}

func TestRetirementObservationFailurePreservesDeadlineAndOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		inventory bool
		deadline  bool
	}{
		{"inventory_deadline", true, true},
		{"sessions_deadline", false, true},
		{"inventory_failure", true, false},
		{"sessions_failure", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, c := newFixture(t)
			engine := &retirementObservationEngine{
				fixture: f, inventoryFailure: test.inventory, waitForDeadline: test.deadline,
			}
			c.Engine, c.Sessions = engine, engine
			err := c.Run(t.Context())
			require.ErrorIs(t, err, replacement.ErrUnknown)
			if test.deadline {
				require.ErrorIs(t, err, replacement.ErrDeadline)
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.NotErrorIs(t, err, replacement.ErrDeadline)
			}
			require.Equal(t, replacement.StateBlocked, f.ledger.State)
			require.Zero(t, f.createCalls)
			require.Zero(t, f.killCalls)
			require.NotEmpty(t, f.inventory)
		})
	}
}
