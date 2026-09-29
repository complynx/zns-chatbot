package conversation_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestHistoryDerivedOrderMutationSharesDeletionLock(t *testing.T) {
	t.Parallel()
	s := historyDatabase(t)
	eventID := longEvent(t, s, "private business draft source")
	deleting, barrier := barrierService(t, s, "SELECT version FROM core.conversation_summaries")
	done := make(chan error, 1)
	go func() { done <- deleting.DeleteContent(t.Context(), "alice", eventID) }()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not acquire owner lock")
	}
	generation := int64(0)
	command := orders.Command{
		EventID:           "sandbox-festival",
		Name:              "create",
		Origin:            "agent",
		Key:               "derived-order",
		HistoryGeneration: &generation,
		Choice:            &orders.ChoiceInput{Customer: "draft source"},
	}
	service := orders.Service{DB: s.DB}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	_, err := service.Execute(ctx, "alice", command)
	cancel()
	close(barrier.release)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, <-done)
	_, err = service.Execute(t.Context(), "alice", command)
	require.ErrorContains(t, err, "history_stale")
	generation = 1
	committed, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	other := longEvent(t, s, "later source")
	require.NoError(t, s.DeleteContent(t.Context(), "alice", other))
	replay, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err, "committed operation receipt survives history deletion")
	assert.Equal(t, committed.ID, replay.ID)
	current, err := service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, current, 1)
	assert.Equal(t, "draft source", current[0].Choice.Customer)
	command.Key = "manual-order"
	command.Origin = "manual"
	command.HistoryGeneration = nil
	_, err = service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
}
