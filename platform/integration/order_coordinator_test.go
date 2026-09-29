package integration_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

type coordinatorHost struct {
	plan            interaction.SavedPlan
	plans           atomic.Int32
	clearError      error
	validationError error
	entered         chan<- struct{}
	release         <-chan struct{}
}

func (*coordinatorHost) Prepare(context.Context) (conversation.Window, error) {
	return conversation.Window{Generation: 0}, nil
}

func (h *coordinatorHost) Plan(ctx context.Context, _ conversation.Window) (interaction.SavedPlan, error) {
	h.plans.Add(1)
	if h.entered != nil {
		h.entered <- struct{}{}
		select {
		case <-h.release:
		case <-ctx.Done():
			return interaction.SavedPlan{}, ctx.Err()
		}
	}
	return h.plan, nil
}

func (h *coordinatorHost) Validate(context.Context, interaction.SavedPlan) error {
	return h.validationError
}
func (h *coordinatorHost) ClearAV(context.Context, []string) error { return h.clearError }

func coordinatorPlan(text string) interaction.SavedPlan {
	return interaction.SavedPlan{
		Plan: agent.Plan{Text: text},
		PassAuthority: &interaction.PlanAuthority{
			ReadAuthorities: []readsource.Authority{},
			Reads:           []interaction.PassContextDependency{},
		},
	}
}

func TestTurnCoordinatorCleanupRetryRestoresWinnerWithoutModel(t *testing.T) {
	t.Parallel()
	f := setup(t)
	coordinator := interaction.TurnCoordinator{Store: interaction.Store{DB: f.db}}
	cleanupFailure := errors.New("temporary AV cleanup failure")
	host := &coordinatorHost{plan: coordinatorPlan("saved answer"), clearError: cleanupFailure}
	_, replay, err := coordinator.ResumeOrPlan(t.Context(), "alice", 981001, host)
	require.ErrorIs(t, err, cleanupFailure)
	require.False(t, replay)
	require.EqualValues(t, 1, host.plans.Load())
	host.clearError = nil
	host.plan = coordinatorPlan("must not replace saved answer")
	restarted := interaction.TurnCoordinator{Store: interaction.Store{DB: f.db}}
	winner, replay, err := restarted.ResumeOrPlan(t.Context(), "alice", 981001, host)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, "saved answer", winner.Plan.Text)
	require.EqualValues(t, 1, host.plans.Load())
	host.validationError = errors.New("authority temporarily unavailable")
	_, replay, err = restarted.ResumeOrPlan(t.Context(), "alice", 981001, host)
	require.ErrorIs(t, err, host.validationError)
	require.True(t, replay)
	require.EqualValues(t, 1, host.plans.Load())
}

func TestTurnCoordinatorConcurrentPlannersReturnOneDurableWinner(t *testing.T) {
	t.Parallel()
	f := setup(t)
	coordinator := interaction.TurnCoordinator{Store: interaction.Store{DB: f.db}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	hosts := []*coordinatorHost{
		{plan: coordinatorPlan("first"), entered: entered, release: release},
		{plan: coordinatorPlan("second"), entered: entered, release: release},
	}
	var group sync.WaitGroup
	winners := make([]interaction.SavedPlan, 2)
	failures := make([]error, 2)
	for index, host := range hosts {
		group.Go(func() {
			winners[index], _, failures[index] = coordinator.ResumeOrPlan(ctx, "alice", 981002, host)
		})
	}
	for range hosts {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("planners did not reach winner race", ctx.Err())
		}
	}
	close(release)
	group.Wait()
	for _, err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, winners[0], winners[1])
	persisted, err := (interaction.Store{DB: f.db}).Load(t.Context(), "alice", 981002)
	require.NoError(t, err)
	require.Equal(t, persisted, winners[0])
	for _, host := range hosts {
		require.EqualValues(t, 1, host.plans.Load())
	}
}
