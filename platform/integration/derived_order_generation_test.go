package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func savedDerivedOrder(t *testing.T) (*fixture, orders.Order, orders.Command, int64) {
	t.Helper()
	f, original := boundOrderFixture(t)
	history := conversation.Service{DB: f.db}
	require.NoError(t, history.AppendOriginal(t.Context(), "alice", "derived-order-source", "user", "add preparty"))
	var sourceID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='derived-order-source'`).
			Scan(&sourceID),
	)
	wire := &orderRestartTransport{window: "saved", armed: true}
	f.b.API.HTTP = &http.Client{Transport: wire}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), message(88001, 101, "add preparty to order "+original.ID)))
	var saved struct {
		Command orders.Command `json:"order_command"`
	}
	require.NoError(t, json.Unmarshal([]byte(savedOrderPlan(t, f)), &saved))
	require.NotNil(
		t,
		saved.Command.HistoryGeneration,
		"ordinary model commands need the same commit fence as script commands",
	)
	require.Zero(t, *saved.Command.HistoryGeneration, "generation zero is explicit authority")
	wire.armed = false
	return f, original, saved.Command, sourceID
}

func TestDerivedOrderGenerationDeletionWinsBeforeCommit(t *testing.T) {
	t.Parallel()
	f, original, command, sourceID := savedDerivedOrder(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := f.db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	var blockerPID int32
	require.NoError(t, blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
	_, err = blocker.Exec(ctx, `SELECT id FROM core.order_events WHERE id=$1 FOR UPDATE`, original.EventID)
	require.NoError(t, err)
	coordinator := interaction.OrderCoordinator{Client: f.b.API, Store: interaction.Store{DB: f.db}}
	type execution struct {
		outcome interaction.OrderOutcome
		err     error
	}
	finished := make(chan execution, 1)
	go func() {
		outcome, runErr := coordinator.Execute(ctx, "alice", 88001, command)
		finished <- execution{outcome, runErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := f.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event_type='Lock' AND query LIKE '%core.order_events%' AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(ctx, "alice", sourceID))
	require.NoError(t, blocker.Commit(ctx))
	select {
	case result := <-finished:
		require.NoError(t, result.err)
		require.NotNil(t, result.outcome.Refusal)
		require.Equal(t, "history_stale", result.outcome.Refusal.Code)
	case <-ctx.Done():
		t.Fatal("order did not finish after lock release", ctx.Err())
	}
	current, err := (orders.Service{DB: f.db}).Get(ctx, "alice", original.EventID, original.ID)
	require.NoError(t, err)
	require.Equal(t, original.Version, current.Version)
	require.NotContains(t, current.Choice.Extras, "preparty")
	require.Zero(t, orderReceiptCount(t, f))
	retry, err := coordinator.Execute(ctx, "alice", 88001, command)
	require.NoError(t, err)
	require.NotNil(t, retry.Refusal)
	require.Equal(t, "history_stale", retry.Refusal.Code)
}

func TestDerivedOrderGenerationCommittedReceiptAndManualSurviveDeletion(t *testing.T) {
	t.Parallel()
	f, original, command, sourceID := savedDerivedOrder(t)
	coordinator := interaction.OrderCoordinator{Client: f.b.API, Store: interaction.Store{DB: f.db}}
	first, err := coordinator.Execute(t.Context(), "alice", 88001, command)
	require.NoError(t, err)
	require.Nil(t, first.Refusal)
	require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", sourceID))
	replay, err := coordinator.Execute(t.Context(), "alice", 88001, command)
	require.NoError(t, err)
	require.Nil(t, replay.Refusal)
	require.Equal(t, first.Order, replay.Order)
	require.Equal(t, 1, orderReceiptCount(t, f))
	manual := orders.Command{Name: "create", EventID: original.EventID, Origin: "manual", Choice: &orders.ChoiceInput{}}
	created, err := coordinator.Execute(t.Context(), "alice", 88002, manual)
	require.NoError(t, err)
	require.Nil(t, created.Refusal)
	require.NotEmpty(t, created.Order.ID)
	require.Nil(t, manual.HistoryGeneration)
}

type orderBindingOutage struct {
	fail    atomic.Bool
	orderID string
}

func (wire *orderBindingOutage) RoundTrip(request *http.Request) (*http.Response, error) {
	if wire.fail.Load() && request.Method == http.MethodGet &&
		strings.HasSuffix(request.URL.Path, "/orders/"+wire.orderID) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestOrderBindingOutageRetriesWithoutSavedFailureNotice(t *testing.T) {
	t.Parallel()
	f, original := boundOrderFixture(t)
	wire := &orderBindingOutage{orderID: original.ID}
	wire.fail.Store(true)
	f.b.API.HTTP = &http.Client{Transport: wire}
	f.b.Host.HTTP = f.b.API.HTTP
	update := message(88001, 101, "add preparty to order "+original.ID)
	require.ErrorIs(t, f.b.Handle(t.Context(), update), interaction.ErrOrderReadUnavailable)
	var saved int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=88001`).
			Scan(&saved),
	)
	require.Zero(t, saved, "infrastructure outage must not become a completed paid failure notice")
	require.Zero(t, orderReceiptCount(t, f))
	wire.fail.Store(false)
	require.NoError(t, f.b.Handle(t.Context(), update))
	current, err := (orders.Service{DB: f.db}).Get(t.Context(), "alice", original.EventID, original.ID)
	require.NoError(t, err)
	require.Equal(t, original.Version+1, current.Version)
	require.Contains(t, current.Choice.Extras, "preparty")
	require.Equal(t, 1, orderReceiptCount(t, f))
}

func TestDerivedOrderGenerationBindsNewCommandsBeforeSave(t *testing.T) {
	t.Parallel()
	f := setup(t)
	turns := interaction.TurnCoordinator{Store: interaction.Store{DB: f.db}}
	for index, name := range []string{"create", "edit"} {
		updateID := int64(88200 + index)
		untrustedGeneration := int64(99)
		proposal := orders.Command{Name: name, Origin: "agent", HistoryGeneration: &untrustedGeneration}
		plan := coordinatorPlan("ordinary order proposal")
		plan.OrderCommand = &proposal
		host := &coordinatorHost{plan: plan}
		saved, replay, err := turns.ResumeOrPlan(t.Context(), "alice", updateID, host)
		require.NoError(t, err)
		require.False(t, replay)
		require.NotNil(t, saved.OrderCommand.HistoryGeneration)
		require.Zero(t, *saved.OrderCommand.HistoryGeneration)
		require.EqualValues(t, 99, *proposal.HistoryGeneration, "binding must not mutate host/model-owned proposal")
		restored, replay, err := turns.ResumeOrPlan(t.Context(), "alice", updateID, host)
		require.NoError(t, err)
		require.True(t, replay)
		require.Equal(t, saved.OrderCommand, restored.OrderCommand)
		require.EqualValues(t, 1, host.plans.Load())
	}
}
