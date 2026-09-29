package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestOrderVerifierOutageRetriesSavedCommand(t *testing.T) {
	t.Parallel()
	f, original := boundOrderFixture(t)
	client := localOrderClient(t, f)
	// Other domains still use the fixture's normal authenticated HTTP service.
	client.HTTP = f.b.API.HTTP
	f.b.API = client
	available := true
	outages := 0
	client.LocalOrders.Authorizer.Verify = func(ctx context.Context, token string) (string, error) {
		if !available {
			var persisted bool
			err := f.db.QueryRow(
				ctx,
				`SELECT EXISTS(SELECT 1 FROM interaction.saved_turns WHERE owner='alice' AND update_id=88001)`,
			).Scan(&persisted)
			if err != nil {
				return "", err
			}
			if persisted {
				outages++
				return "", errors.New("temporary verifier failure")
			}
		}
		return f.b.Host.Signer.Verify(token)
	}
	f.b.Host.LocalDerived = &appclient.LocalDerived{
		Service:    derivedmutation.Service{DB: f.db, Orders: client.LocalOrders.Service},
		Authorizer: client.LocalOrders.Authorizer,
	}
	modelCalls := 0
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		modelCalls++
		available = false
		return f.model.plan, nil
	})
	update := message(88001, 101, "add preparty to order "+original.ID)
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Positive(t, outages, "live verifier outage must be injected before mutation")
	require.Zero(t, orderReceiptCount(t, f))
	var refusals int
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT count(*) FROM bot.interactions WHERE update_id=88001 AND kind='order_error'`).Scan(&refusals))
	require.Zero(t, refusals)
	before := savedOrderPlan(t, f)
	require.NotEmpty(t, before)
	available = true
	restartedModel := restartOrderBot(f)
	require.NoError(t, f.b.Handle(t.Context(), update))
	require.Equal(t, 1, orderReceiptCount(t, f))
	require.Equal(t, 1, modelCalls)
	require.Zero(t, restartedModel.calls)
	current, err := (orders.Service{DB: f.db}).Get(t.Context(), "alice", original.EventID, original.ID)
	require.NoError(t, err)
	require.Equal(t, original.Version+1, current.Version)
	require.Contains(t, current.Choice.Extras, "preparty")
	require.NoError(t, f.b.Handle(t.Context(), update))
	require.Equal(t, 1, orderReceiptCount(t, f))
}
