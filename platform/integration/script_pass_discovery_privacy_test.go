package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestScriptPassDiscoveryRevocation(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET titles='{"en":"private-archive-membership-canary"}' WHERE id='archive'`,
	)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `const p=await tools.passes.events({}); return p.items.map(e=>e.titles);`,
					InputJSON: "null",
				},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[0].Result), "private-archive-membership-canary")
		return agent.Plan{}, context.Canceled
	})
	update := message(29990, 101, "List my historical events")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.legacy_pass_payment_metadata WHERE event_id='archive'; DELETE FROM core.pass_bookings WHERE event_id='archive' AND owner='alice'`,
	)
	require.NoError(t, err)
	var observed agent.Input
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		observed = input
		return agent.Plan{}, context.Canceled
	})
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	raw, err := json.Marshal(observed.Script)
	require.NoError(t, err)
	t.Logf("Retry script context: %s", raw)
	require.NotContains(t, string(raw), "private-archive-membership-canary")
}
