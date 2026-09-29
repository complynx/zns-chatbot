package replacement_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
)

type cancellationEngine struct {
	*fixture

	cancel       context.CancelFunc
	blockKill    bool
	stopDeadline time.Time
	killDeadline time.Time
	stopErr      error
	killEntryErr error
}

func (e *cancellationEngine) Inventory(ctx context.Context) ([]replacement.Container, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.fixture.Inventory(ctx)
}

func (e *cancellationEngine) Stop(ctx context.Context, _ []replacement.Container) error {
	e.stopDeadline, _ = ctx.Deadline()
	e.cancel()
	<-ctx.Done()
	e.stopErr = ctx.Err()
	return e.stopErr
}

func (e *cancellationEngine) Kill(ctx context.Context, containers []replacement.Container) error {
	e.killEntryErr = ctx.Err()
	e.killDeadline, _ = ctx.Deadline()
	if e.killEntryErr != nil {
		return e.killEntryErr
	}
	if e.blockKill {
		<-ctx.Done()
		return ctx.Err()
	}
	return e.fixture.Kill(ctx, containers)
}

func TestInitialRetirementFinishesAfterCallerCancellation(t *testing.T) {
	t.Parallel()
	for _, blockedKill := range []bool{false, true} {
		t.Run(strconv.FormatBool(blockedKill), func(t *testing.T) {
			t.Parallel()
			f, coordinator := newFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			engine := &cancellationEngine{fixture: f, cancel: cancel, blockKill: blockedKill}
			coordinator.Engine = engine
			coordinator.StopTimeout = 20 * time.Millisecond
			coordinator.VerifyTimeout = 100 * time.Millisecond
			started := time.Now()

			err := coordinator.Run(ctx)

			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, engine.stopErr, context.DeadlineExceeded)
			require.NoError(t, engine.killEntryErr, "caller cancellation must not cancel owned cleanup")
			require.False(t, engine.stopDeadline.IsZero())
			require.False(t, engine.killDeadline.IsZero())
			require.False(t, engine.killDeadline.After(engine.stopDeadline.Add(coordinator.VerifyTimeout)))
			require.Less(t, time.Since(started), time.Second, "context-aware failed shutdown must remain bounded")
			require.Zero(t, f.createCalls)
			require.Zero(t, f.startCalls)
			if blockedKill {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, replacement.StateBlocked, f.ledger.State)
				require.True(t, f.inventory[0].Running)
				return
			}
			require.Equal(t, replacement.StateStopped, f.ledger.State)
			require.Empty(t, f.inventory)
			require.Empty(t, f.names)
		})
	}
}
