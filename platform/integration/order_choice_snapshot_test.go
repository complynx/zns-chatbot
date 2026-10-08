package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestOrderChoiceSnapshotBoundary(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := orders.Service{DB: f.db}
	original, err := service.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "snapshot-boundary",
		Choice: &orders.ChoiceInput{Customer: "PRIVATE Ж<& choice"},
	})
	require.NoError(t, err)
	event, err := f.b.API.OrderEvent(t.Context(), "alice", original.EventID)
	require.NoError(t, err)
	order, err := f.b.API.Order(t.Context(), "alice", original.EventID, original.ID)
	require.NoError(t, err)
	// Match the historical host algorithm byte for byte, including decoded HTTP times.
	raw, err := json.Marshal(order)
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	expected := orders.ChoiceSnapshot{
		Catalog: orders.CatalogSnapshot(event),
		Order:   hex.EncodeToString(digest[:]),
		CanBook: true,
	}
	var envelope map[string]json.RawMessage
	require.NoError(
		t,
		f.b.API.Call(
			t.Context(),
			"alice",
			http.MethodGet,
			"/v1/order-events/"+original.EventID+"/choice-snapshot?order_id="+original.ID,
			nil,
			&envelope,
		),
	)
	require.Len(t, envelope, 3)
	require.Contains(t, envelope, "catalog")
	require.Contains(t, envelope, "order")
	require.Contains(t, envelope, "can_book")
	for _, client := range []appclient.Client{f.b.API, localOrderClient(t, f)} {
		result, readErr := client.OrderChoiceSnapshot(t.Context(), "alice", original.EventID, original.ID)
		require.NoError(t, readErr)
		require.Equal(t, expected, result)
		denied, denyErr := client.OrderChoiceSnapshot(t.Context(), "bob", original.EventID, original.ID)
		require.Error(t, denyErr)
		require.Equal(t, orders.ChoiceSnapshot{}, denied)
		catalog, catErr := client.OrderChoiceSnapshot(t.Context(), "alice", original.EventID, "")
		require.NoError(t, catErr)
		require.Equal(t, orders.ChoiceSnapshot{Catalog: expected.Catalog, CanBook: true}, catalog)
	}
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{f.b.API, localOrderClient(t, f)} {
		current, readErr := client.OrderChoiceSnapshot(t.Context(), "alice", original.EventID, original.ID)
		require.NoError(t, readErr)
		require.False(t, current.CanBook)
		require.Equal(t, expected.Order, current.Order)
	}
}
