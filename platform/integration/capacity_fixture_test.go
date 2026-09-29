package integration_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestCapacityFixturePreservesReservationsAndEnablesDisplacement(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := orders.Service{DB: db}
	const extra = "excursion_grodno_overview"
	create := func(owner, key string) orders.Order {
		t.Helper()
		choice := orderChoice(extra)
		choice.Extras["preparty"] = json.RawMessage(`0`)
		order, err := service.Execute(t.Context(), owner, orders.Command{
			EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: key, Choice: choice,
		})
		require.NoError(t, err)
		return order
	}
	reserve := func(order orders.Order) orders.Order {
		t.Helper()
		command := orderCommand("proof", order)
		command.ProofFile = uploadProof(t, service, order.Owner)
		result, err := service.Execute(t.Context(), order.Owner, command)
		require.NoError(t, err)
		return result
	}
	existing := reserve(create("alice", "existing"))
	fixture := sandbox.OrderFixture{Capacity: &sandbox.CapacityFixture{Extra: extra, Remaining: 1}}
	require.NoError(t, sandbox.ApplyOrderFixture(t.Context(), db, fixture))
	event, err := service.Event(t.Context(), "sandbox-festival")
	require.NoError(t, err)
	assert.Equal(t, 2, event.Extras[extra].Capacity)
	displaced := create("bob", "displaced")
	reserve(create("alice", "last-seat"))
	unchanged, err := service.Get(t.Context(), "alice", event.ID, existing.ID)
	require.NoError(t, err)
	assert.Equal(t, existing, unchanged)
	changed, err := service.Get(t.Context(), "bob", event.ID, displaced.ID)
	require.NoError(t, err)
	assert.NotContains(t, changed.Choice.Extras, extra)
	assert.Equal(t, orders.Money(3500), changed.Choice.Total)
	notices, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, "capacity", notices[0].Kind)
	assert.Equal(t, displaced.ID, notices[0].OrderID)
	assert.Equal(t, []string{extra}, notices[0].Removed)
	fixture.Capacity = &sandbox.CapacityFixture{Extra: extra, Reset: true}
	require.NoError(t, sandbox.ApplyOrderFixture(t.Context(), db, fixture))
	event, err = service.Event(t.Context(), event.ID)
	require.NoError(t, err)
	assert.Equal(t, orders.Extras()[extra].Capacity, event.Extras[extra].Capacity)
}

func TestCapacityFixtureRejectsInvalidControls(t *testing.T) {
	t.Parallel()
	db := database(t)
	for _, capacity := range []sandbox.CapacityFixture{
		{Extra: "unknown", Remaining: 1},
		{Extra: "preparty", Remaining: 1},
		{Extra: "shuttle", Remaining: 0},
		{Extra: "shuttle", Remaining: -1},
		{Extra: "shuttle", Remaining: 1001},
		{Extra: "shuttle", Remaining: 1, Reset: true},
	} {
		require.Error(t, sandbox.ApplyOrderFixture(t.Context(), db, sandbox.OrderFixture{Capacity: &capacity}))
	}
	event, err := (orders.Service{DB: db}).Event(t.Context(), "sandbox-festival")
	require.NoError(t, err)
	assert.Equal(t, orders.Extras(), event.Extras)
}

func TestCapacityResetCannotDisplaceExistingReservations(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := orders.Service{DB: db}
	const extra = "excursion_grodno_overview"
	count := orders.Extras()[extra].Capacity + 1
	require.NoError(t, sandbox.ApplyOrderFixture(t.Context(), db, sandbox.OrderFixture{
		Capacity: &sandbox.CapacityFixture{Extra: extra, Remaining: count},
	}))
	for index := range count {
		order, err := service.Execute(t.Context(), "alice", orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     strconv.Itoa(index),
			Choice:  orderChoice(extra),
		})
		require.NoError(t, err)
		command := orderCommand("proof", order)
		command.ProofFile = uploadProof(t, service, "alice")
		_, err = service.Execute(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	before, err := service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.ErrorContains(t, sandbox.ApplyOrderFixture(t.Context(), db, sandbox.OrderFixture{
		Capacity: &sandbox.CapacityFixture{Extra: extra, Reset: true},
	}), "would remove reservations")
	event, err := service.Event(t.Context(), "sandbox-festival")
	require.NoError(t, err)
	assert.Equal(t, count, event.Extras[extra].Capacity)
	after, err := service.List(t.Context(), "alice", event.ID)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
