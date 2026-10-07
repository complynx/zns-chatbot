package replacement_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const installation = "abcdef012345"
const oldLaunch = "111111111111111111111111"
const newLaunch = "222222222222222222222222"

type fixture struct {
	ledger       replacement.Ledger
	inventory    []replacement.Container
	names        []string
	events       []string
	inventoryErr error
	sessionErr   error
	daemon       string
	keepSession  bool
	keepProcess  bool
	stopErr      error
	killCalls    int
	overlap      bool
	unsafeStart  bool
	createCalls  int
	startCalls   int
	onStart      func()
	onRunning    func()
	saveFailure  string
}

func (f *fixture) Identity(context.Context) (string, error) { return f.daemon, nil }
func (f *fixture) Inventory(context.Context) ([]replacement.Container, error) {
	return slices.Clone(f.inventory), f.inventoryErr
}
func (f *fixture) Names(context.Context) ([]string, error) {
	return slices.Clone(f.names), f.sessionErr
}
func (f *fixture) Load() (replacement.Ledger, error) { return f.ledger, nil }
func (f *fixture) Save(ledger replacement.Ledger) error {
	f.events = append(f.events, "save:"+ledger.State)
	if f.saveFailure == ledger.State {
		return errors.New("durability failure")
	}
	ledger.Containers = slices.Clone(ledger.Containers)
	f.ledger = ledger
	if ledger.State == replacement.StateRunning && f.onRunning != nil {
		f.onRunning()
	}
	return nil
}
func (f *fixture) Create(_ context.Context, instance runtimeapp.Instance) ([]replacement.Container, error) {
	for _, item := range f.inventory {
		f.overlap = f.overlap || item.Running || item.Restarting || item.Paused || item.PID != 0
	}
	f.overlap = f.overlap || len(f.names) != 0
	f.unsafeStart = f.unsafeStart || f.ledger.State != replacement.StateStarting || f.ledger.Launch != instance.Launch
	f.createCalls++
	f.events = append(f.events, "create")
	f.inventory = nil
	for _, component := range replacement.Components() {
		f.inventory = append(f.inventory, replacement.Container{
			ID:        component + "-new",
			Component: component,
			Launch:    instance.Launch,
			Image:     "digest",
			Created:   "now",
			Health:    "healthy",
		})
	}
	return slices.Clone(f.inventory), nil
}
func (f *fixture) Start(context.Context, []replacement.Container) error {
	f.unsafeStart = f.unsafeStart || f.ledger.State != replacement.StateStarting ||
		len(f.ledger.Containers) != len(f.inventory)
	f.startCalls++
	f.events = append(f.events, "start")
	for i := range f.inventory {
		f.inventory[i].Running = true
		f.inventory[i].PID = 1
	}
	if f.onStart != nil {
		f.onStart()
	}
	return nil
}
func (f *fixture) Stop(context.Context, []replacement.Container) error {
	f.events = append(f.events, "stop")
	if f.stopErr != nil {
		return f.stopErr
	}
	if !f.keepProcess {
		for i := range f.inventory {
			f.inventory[i].Running = false
			f.inventory[i].PID = 0
		}
	}
	if !f.keepSession {
		f.names = nil
	}
	return nil
}
func (f *fixture) Kill(context.Context, []replacement.Container) error {
	f.killCalls++
	return errors.New("forced termination is not graceful retirement")
}
func (f *fixture) Remove(context.Context, []replacement.Container) error {
	f.events = append(f.events, "remove")
	f.inventory = nil
	return nil
}

func newFixture(t *testing.T) (*fixture, *replacement.Coordinator) {
	t.Helper()
	old := replacement.Container{
		ID:        "old-app",
		Component: "app",
		Launch:    oldLaunch,
		Image:     "digest",
		Created:   "before",
		Running:   true,
		PID:       1,
	}
	name, err := (runtimeapp.Instance{Installation: installation, Launch: oldLaunch}).ApplicationName("app")
	require.NoError(t, err)
	f := &fixture{daemon: "daemon", inventory: []replacement.Container{old}, names: []string{name},
		ledger: replacement.Ledger{
			Version:      1,
			Installation: installation,
			Host:         "host",
			Daemon:       "daemon",
			Generation:   1,
			Launch:       oldLaunch,
			State:        replacement.StateRunning,
			Containers:   []replacement.Container{old},
		},
	}
	return f, &replacement.Coordinator{
		Engine:        f,
		Sessions:      f,
		Journal:       f,
		Installation:  installation,
		Host:          "host",
		StopTimeout:   time.Second,
		VerifyTimeout: 10 * time.Millisecond,
		PollInterval:  time.Millisecond,
		ReadyTimeout:  time.Second,
		NewLaunch:     func() (string, error) { return newLaunch, nil },
	}
}

func TestReplacementWaitsForProcessesAndSessionsBeforeStarting(t *testing.T) {
	t.Parallel()
	f, c := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.onStart = cancel
	require.NoError(t, c.Run(ctx))
	require.Equal(t, 1, f.startCalls)
	require.Equal(t, replacement.StateStopped, f.ledger.State)
	require.False(t, f.overlap, "old processes and sessions must be absent before create")
	require.False(t, f.unsafeStart, "the new generation must be durable before starting")
	require.Zero(t, f.killCalls)
	require.Empty(t, f.inventory)
}

func TestReplacementRunningAdmissionLossRetiresGeneration(t *testing.T) {
	t.Parallel()
	f, c := newFixture(t)
	instance := runtimeapp.Instance{Installation: installation, Launch: newLaunch}
	admit, err := instance.ApplicationName("admit")
	require.NoError(t, err)
	app, err := instance.ApplicationName("app")
	require.NoError(t, err)
	f.onStart = func() { f.names = []string{admit, app, app} }
	f.onRunning = func() { f.names = []string{app, app} }
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, c.Run(ctx), replacement.ErrStopped)
	require.Equal(t, 1, f.createCalls)
	require.Equal(t, replacement.StateStopped, f.ledger.State)
	require.Empty(t, f.inventory)
	require.Empty(t, f.names)
	require.Equal(t, uint64(2), f.ledger.Generation)
	require.False(t, f.overlap)
	require.False(t, f.unsafeStart)
	require.Zero(t, f.killCalls)
}

func TestReplacementAdmissionReadiness(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"startup-missing", "duplicate", "healthy"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, c := newFixture(t)
			c.ReadyTimeout = 20 * time.Millisecond
			instance := runtimeapp.Instance{Installation: installation, Launch: newLaunch}
			admit, err := instance.ApplicationName("admit")
			require.NoError(t, err)
			app, err := instance.ApplicationName("app")
			require.NoError(t, err)
			media, err := instance.ApplicationName("media")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			f.onStart = func() {
				f.names = []string{app, app, media}
				if scenario != "startup-missing" {
					f.names = append(f.names, admit)
				}
				if scenario == "duplicate" {
					f.names = append(f.names, admit)
				}
			}
			f.onRunning = cancel
			err = c.Run(ctx)
			switch scenario {
			case "startup-missing":
				require.ErrorIs(t, err, replacement.ErrDeadline)
				require.NotContains(t, f.events, "save:running")
			case "duplicate":
				require.ErrorIs(t, err, replacement.ErrUnknown)
				require.NotContains(t, f.events, "save:running")
			case "healthy":
				require.NoError(t, err)
				require.Contains(t, f.events, "save:running")
			}
			require.Equal(t, replacement.StateStopped, f.ledger.State)
		})
	}
}

func TestReplacementAdmissionLossKeepsIncompleteBarrierBlocked(t *testing.T) {
	t.Parallel()
	f, c := newFixture(t)
	instance := runtimeapp.Instance{Installation: installation, Launch: newLaunch}
	admit, err := instance.ApplicationName("admit")
	require.NoError(t, err)
	app, err := instance.ApplicationName("app")
	require.NoError(t, err)
	f.onStart = func() { f.names = []string{admit, app} }
	f.onRunning = func() { f.names = []string{app}; f.keepSession = true }
	err = c.Run(t.Context())
	require.ErrorIs(t, err, replacement.ErrStopped)
	require.ErrorIs(t, err, replacement.ErrDeadline)
	require.Equal(t, replacement.StateBlocked, f.ledger.State)
	require.Equal(t, 1, f.createCalls)
	require.ErrorIs(t, c.Run(t.Context()), replacement.ErrDeadline)
	require.Equal(t, 1, f.createCalls, "remaining old sessions must prevent another launch")
}

func TestReplacementFailsClosedOnIncompleteBarrier(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"session", "process", "stop-error", "unknown-session", "unknown-container", "database", "docker", "host", "daemon", "durability", "stopped-durability", "collision"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			f, c := newFixture(t)
			switch reason {
			case "session":
				f.keepSession = true
			case "process":
				f.keepProcess = true
			case "stop-error":
				f.stopErr = errors.New("graceful stop failed")
			case "unknown-session":
				f.keepSession = true
				f.names = []string{"untagged"}
			case "unknown-container":
				f.inventory[0].ID = "unowned"
			case "database":
				f.sessionErr = errors.New("database unavailable")
			case "docker":
				f.inventoryErr = errors.New("Docker unavailable")
			case "host":
				c.Host = "different-host"
			case "daemon":
				f.daemon = "different-daemon"
			case "durability":
				f.saveFailure = replacement.StateStopping
			case "stopped-durability":
				f.saveFailure = replacement.StateStopped
			case "collision":
				c.NewLaunch = func() (string, error) { return oldLaunch, nil }
			}
			err := c.Run(t.Context())
			require.Error(t, err)
			require.Zero(t, f.startCalls)
			require.Zero(t, f.createCalls)
			require.Zero(t, f.killCalls)
			if reason == "stop-error" {
				require.ErrorIs(t, err, f.stopErr)
				require.Equal(t, replacement.StateBlocked, f.ledger.State)
				require.True(t, f.inventory[0].Running)
				require.Equal(t, oldLaunch, f.ledger.Launch)
				require.Len(t, f.ledger.Containers, 1)
			}
		})
	}
}

func TestReplacementRetiresGroupOnHelperCrash(t *testing.T) {
	t.Parallel()
	f, c := newFixture(t)
	f.onStart = func() { f.inventory[0].Running = false }
	require.ErrorIs(t, c.Run(t.Context()), replacement.ErrStopped)
	require.Equal(t, 1, f.startCalls)
	require.Empty(t, f.inventory)
	require.Equal(t, replacement.StateStopped, f.ledger.State)
}

func TestReplacementReconcilesInterruptedKnownGeneration(t *testing.T) {
	t.Parallel()
	for _, state := range []string{replacement.StateStarting, replacement.StateRunning, replacement.StateStopping, replacement.StateBlocked} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f, c := newFixture(t)
			f.ledger.State = state
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.onStart = cancel
			require.NoError(t, c.Run(ctx))
			require.Equal(t, uint64(2), f.ledger.Generation)
		})
	}
}
