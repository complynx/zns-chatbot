package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestDirectAPIHistoryReachesAgentAndSurvivesDeletion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	client := f.b.API
	created, err := client.ExecuteOrder(ctx, "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "create", Choice: orderChoice("shuttle"),
	})
	require.NoError(t, err)
	edit := orderCommand("edit", created)
	edit.Choice = orderChoice("preparty")
	edited, err := client.ExecuteOrder(ctx, "alice", edit)
	require.NoError(t, err)
	_, err = client.ExecuteOrder(ctx, "alice", edit)
	require.NoError(t, err, "retry must not duplicate history")
	stale := edit
	stale.Key = "stale-edit"
	_, err = client.ExecuteOrder(ctx, "alice", stale)
	requireCode(t, err, "stale_version")
	f.model.plan = agent.Plan{View: "orders"}
	handle(t, f.b, message(100, 101, "What did I remove from my order?"))
	changes := f.model.input.OrderHistory
	require.Len(t, changes, 2)
	assert.Equal(t, created.ID, changes[1].OrderID)
	assert.Equal(t, "manual", changes[1].Origin)
	assert.Equal(t, orders.Money(6500), changes[1].BeforeExtras["shuttle"])
	assert.NotContains(t, changes[1].Extras, "shuttle")
	assert.Equal(t, orders.Money(3500), changes[1].Total)
	assert.False(t, changes[1].At.IsZero())
	for _, owner := range []string{"bob", "visitor"} {
		foreign, historyError := client.OrderHistory(ctx, owner, "sandbox-festival")
		require.NoError(t, historyError)
		assert.Empty(t, foreign)
	}
	_, err = client.ExecuteOrder(ctx, "alice", orderCommand("delete", edited))
	require.NoError(t, err)
	changes, err = client.OrderHistory(ctx, "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, changes, 3)
	assert.Equal(t, "deleted", changes[2].State)
}

func TestOrderHistoryKeepsRecentChangesAndCapacityCause(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := orders.Service{DB: db}
	ctx := t.Context()
	command := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "create",
		Choice:  orderChoice("shuttle"),
	}
	alice, err := service.Execute(ctx, "alice", command)
	require.NoError(t, err)
	_, err = service.Execute(ctx, "bob", command)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1')`)
	require.NoError(t, err)
	proof := orderCommand("proof", alice)
	proof.ProofFile = uploadProof(t, service, "alice")
	_, err = service.Execute(ctx, "alice", proof)
	require.NoError(t, err)
	history, err := service.History(ctx, "bob", command.EventID)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, "system", history[1].Origin)
	assert.Equal(t, "capacity", history[1].Action)
	assert.Contains(t, history[1].BeforeExtras, "shuttle")
	assert.NotContains(t, history[1].Extras, "shuttle")
	for range 32 {
		list, listError := service.List(ctx, "bob", command.EventID)
		require.NoError(t, listError)
		edit := orderCommand("edit", list[0])
		edit.Choice = orderChoice("preparty")
		_, err = service.Execute(ctx, "bob", edit)
		require.NoError(t, err)
	}
	history, err = service.History(ctx, "bob", command.EventID)
	require.NoError(t, err)
	require.Len(t, history, 30)
	assert.EqualValues(t, 5, history[0].Version)
	assert.EqualValues(t, 34, history[29].Version)
}
