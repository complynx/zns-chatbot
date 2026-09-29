package integration_test

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

func TestHTTPUsesReadyOrderService(t *testing.T) {
	t.Parallel()
	f := setup(t)
	domain := orders.Service{DB: database(t)}
	order, err := domain.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "injected-domain",
		Choice: &orders.ChoiceInput{Customer: "ready domain service"},
	})
	require.NoError(t, err)
	deps := runtimeapp.NewServices(f.db, runtimeapp.Options{})
	// Authentication still uses the normal Core dependency. The selected order
	// service owns its data; the router must not reconstruct it from Core.DB.
	deps.Orders = domain
	server := httptest.NewServer(api.Handler(deps, f.b.API.Signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	client := f.b.API
	client.Base = server.URL
	actual, err := client.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order, actual)
	_, err = client.Order(t.Context(), "bob", order.EventID, order.ID)
	require.Error(t, err, "ready dependencies do not bypass live domain ownership")
}
