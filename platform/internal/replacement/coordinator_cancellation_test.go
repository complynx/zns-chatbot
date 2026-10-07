package replacement_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

type cancellationEngine struct {
	*fixture

	cancel         context.CancelFunc
	timeout        bool
	lateNames      bool
	stopDeadline   time.Time
	stopEntered    time.Time
	stopEntryErr   error
	verifyDeadline time.Time
}

func (e *cancellationEngine) Names(ctx context.Context) ([]string, error) {
	e.verifyDeadline, _ = ctx.Deadline()
	if e.lateNames {
		<-ctx.Done()
		return nil, nil
	}
	return e.fixture.Names(ctx)
}

func (e *cancellationEngine) Inventory(ctx context.Context) ([]replacement.Container, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.fixture.Inventory(ctx)
}

func (e *cancellationEngine) Stop(ctx context.Context, containers []replacement.Container) error {
	e.stopEntered = time.Now()
	e.stopDeadline, _ = ctx.Deadline()
	e.stopEntryErr = ctx.Err()
	e.cancel()
	if e.timeout {
		<-ctx.Done()
		return ctx.Err()
	}
	return e.fixture.Stop(ctx, containers)
}

func TestInitialRetirementFinishesAfterCallerCancellation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name      string
		timeout   bool
		lateNames bool
		budget    time.Duration
	}{
		{name: "prompt", budget: 20 * time.Millisecond},
		{name: "timeout", timeout: true, budget: 20 * time.Millisecond},
		{name: "late-observation", lateNames: true, budget: 20 * time.Millisecond},
		{name: "cap", budget: time.Minute},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			f, coordinator := newFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			engine := &cancellationEngine{
				fixture:   f,
				cancel:    cancel,
				timeout:   scenario.timeout,
				lateNames: scenario.lateNames,
			}
			coordinator.Engine = engine
			coordinator.Sessions = engine
			coordinator.StopTimeout = scenario.budget
			coordinator.VerifyTimeout = time.Minute
			started := time.Now()

			err := coordinator.Run(ctx)

			require.ErrorIs(t, err, context.Canceled)
			require.NoError(t, engine.stopEntryErr, "caller cancellation must not interrupt owned cleanup")
			require.False(t, engine.stopDeadline.IsZero())
			require.LessOrEqual(t, engine.stopDeadline.Sub(engine.stopEntered), 5*time.Second)
			require.Less(t, time.Since(started), time.Second, "context-aware failed shutdown must remain bounded")
			require.Zero(t, f.createCalls)
			require.Zero(t, f.startCalls)
			require.Zero(t, f.killCalls)
			if scenario.timeout || scenario.lateNames {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, replacement.StateBlocked, f.ledger.State)
				require.Equal(t, scenario.timeout, f.inventory[0].Running)
				require.Len(t, f.ledger.Containers, 1)
				return
			}
			require.Equal(t, replacement.StateStopped, f.ledger.State)
			require.Equal(
				t,
				engine.stopDeadline,
				engine.verifyDeadline,
				"verification must not renew the graceful deadline",
			)
			require.Empty(t, f.inventory)
			require.Empty(t, f.names)
		})
	}
}

type delayedPublication struct {
	*fixture

	stopContext context.Context
	final       bool
}

func (p *delayedPublication) Stop(ctx context.Context, containers []replacement.Container) error {
	p.stopContext = ctx
	return p.fixture.Stop(ctx, containers)
}

func (p *delayedPublication) Save(ledger replacement.Ledger) error {
	if ledger.State == replacement.StateStopped && (len(ledger.Containers) == 0) == p.final {
		<-p.stopContext.Done()
	}
	return p.fixture.Save(ledger)
}

func TestLateStoppedPublicationCannotStartAnotherGeneration(t *testing.T) {
	t.Parallel()
	for _, final := range []bool{false, true} {
		name := "physical-publication"
		if final {
			name = "final-publication"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, coordinator := newFixture(t)
			publication := &delayedPublication{fixture: f, final: final}
			coordinator.Engine, coordinator.Journal = publication, publication
			coordinator.StopTimeout = 20 * time.Millisecond
			coordinator.VerifyTimeout = time.Second

			require.ErrorIs(t, coordinator.Run(t.Context()), context.DeadlineExceeded)
			require.Zero(t, f.createCalls)
			require.Zero(t, f.startCalls)
			require.Zero(t, f.killCalls)
			require.Equal(t, uint64(1), f.ledger.Generation)
			require.Equal(t, oldLaunch, f.ledger.Launch)
			require.Equal(t, replacement.StateStopped, f.ledger.State)
			if final {
				require.Empty(t, f.ledger.Containers)
				require.Empty(t, f.inventory)
				return
			}
			require.Len(t, f.ledger.Containers, 1)
			require.Len(t, f.inventory, 1, "expired publication must leave stopped containers in place")
			require.False(t, f.inventory[0].Running)
		})
	}
}
