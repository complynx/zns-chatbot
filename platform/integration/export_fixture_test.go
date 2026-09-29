package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestExportFixtureHistoricalChoiceAndAdminRevocation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := orders.Service{DB: f.db}
	order, err := service.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Key: "history", Origin: "manual", Choice: orderChoice("preparty"),
	})
	require.NoError(t, err)
	choice := order.Choice
	choice.Customer = "Historical fixture"
	choice.Days = map[string]orders.Day{"friday": {Mealtimes: map[string]orders.Meal{"dinner": {
		Dishes: []orders.Line{{Name: "caesar", Count: 2, Price: 123, Total: 246}}, Total: 246,
	}}, Total: 246}}
	choice.Total = 3746
	fixture := sandbox.ExportFixture{OrderID: order.ID, Choice: &choice}
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), f.db, fixture))
	current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order.Version+1, current.Version)
	assert.Equal(t, choice, current.Choice)
	proof := orderCommand("proof", current)
	proof.ProofFile = uploadProof(t, service, "alice")
	_, err = service.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	require.Error(t, sandbox.ApplyExportFixture(t.Context(), f.db, fixture), "locked payment data cannot be injected")
	body, err := f.b.API.ExportOrders(t.Context(), "bob", order.EventID)
	require.NoError(t, err)
	assert.Contains(t, exportRows(t, openExport(t, body), "Итоги"), []string{"Цезарь", "2", "2.46"})
	enabled := false
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), f.db, sandbox.ExportFixture{AdminEnabled: &enabled}))
	_, err = f.b.API.ExportOrders(t.Context(), "bob", order.EventID)
	requireCode(t, err, "forbidden")
	enabled = true
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), f.db, sandbox.ExportFixture{AdminEnabled: &enabled}))
	_, err = f.b.API.ExportOrders(t.Context(), "bob", order.EventID)
	require.NoError(t, err)
}

func TestExportBatchFixtureIsIsolatedAndRemovable(t *testing.T) {
	t.Parallel()
	f := setup(t)
	fixture := sandbox.ExportFixture{BatchTag: "boundary", BatchCount: 10001}
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), f.db, fixture))
	_, err := f.b.API.ExportOrders(t.Context(), "bob", "qa-export-boundary")
	requireCode(t, err, "export_too_large")
	body, err := f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	assert.Len(t, exportRows(t, openExport(t, body), "Заказы"), 1)
	fixture.BatchCount = 0
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), f.db, fixture))
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), f.db, fixture), "cleanup is repeatable")
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_events`).Scan(&count))
	assert.Equal(t, 1, count)
	for _, tag := range []string{"", "../sandbox-festival", "mixed_TAG", "%"} {
		require.Error(
			t,
			sandbox.ApplyExportFixture(t.Context(), f.db, sandbox.ExportFixture{BatchTag: tag, BatchCount: 1}),
		)
	}
}

func TestExportBatchCleanupRefusesBusinessChanges(t *testing.T) {
	t.Parallel()
	db := database(t)
	fixture := sandbox.ExportFixture{BatchTag: "changed", BatchCount: 1}
	require.NoError(t, sandbox.ApplyExportFixture(t.Context(), db, fixture))
	service := orders.Service{DB: db}
	order, err := service.Get(t.Context(), "alice", "qa-export-changed", "qa-export-changed:1")
	require.NoError(t, err)
	command := orderCommand("edit", order)
	command.EventID, command.Choice = order.EventID, orderChoice("preparty")
	_, err = service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	fixture.BatchCount = 0
	require.ErrorContains(t, sandbox.ApplyExportFixture(t.Context(), db, fixture), "refusing cleanup")
	current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, orders.Money(3500), current.Choice.Total)
}
