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
