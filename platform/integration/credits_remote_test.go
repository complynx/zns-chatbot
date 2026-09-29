package integration_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

type meteredRemoteFixture struct{ service credits.Service }

func (m meteredRemoteFixture) Plan(ctx context.Context, _ agent.Input) (agent.Plan, error) {
	call, err := credits.Begin(ctx, m.service, "test.plan", "openai", "synthetic-bound", 10)
	if err != nil {
		return agent.Plan{}, err
	}
	call.Capture(
		credits.Usage{
			Basis:       "reported",
			Model:       "synthetic-bound",
			ServiceTier: "default",
			Input:       new(int64(10)),
			Cached:      new(int64(0)),
			Output:      new(int64(0)),
		},
	)
	if err = call.Finish(ctx); err != nil {
		return agent.Plan{}, err
	}
	return agent.Plan{Text: "Receipt preserved.", View: "workflow"}, nil
}

func TestCreditsTrustedRemoteAttributionAndReceipt(t *testing.T) {
	t.Parallel()
	s := credits.Service{DB: database(t), Enforce: true}
	installCreditPrice(t, s)
	server := httptest.NewServer(
		agent.AuthenticatedModelHandler(meteredRemoteFixture{s}, "synthetic-shared-secret", true),
	)
	t.Cleanup(server.Close)
	remote := agent.Remote{
		URL:      server.URL,
		HTTP:     server.Client(),
		Secret:   "synthetic-shared-secret",
		Enforce:  true,
		Receipts: s,
	}
	ctx := credits.WithScope(t.Context(), credits.Scope{Actor: "alice", Payer: "alice", Key: "telegram:remote-fixture"})
	_, err := remote.Plan(ctx, agent.Input{})
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts WHERE payer='alice' AND actor='alice' AND operation_key='telegram:remote-fixture' AND state='settled'`).
			Scan(&count),
	)
	require.Equal(t, 1, count, "proxy must not create a second charge")
	remote.Secret = "wrong-secret"
	_, err = remote.Plan(ctx, agent.Input{})
	require.Error(t, err)
	remote.Secret = "synthetic-shared-secret"
	remote.Enforce = false
	_, err = remote.Plan(ctx, agent.Input{})
	require.Error(t, err)
	require.NoError(t, s.DB.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts`).Scan(&count))
	require.Equal(t, 1, count)
}
