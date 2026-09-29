package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestScriptPassInScriptPrivilegedReadRevocation(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment='fresh-qa-private-queue' WHERE owner='alice'`,
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
					Code:      `return tools.passes.admin.queue({event:"dance"});`,
					InputJSON: "null",
				},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[0].Result), "fresh-qa-private-queue")
		return agent.Plan{}, context.Canceled
	})
	update := message(42991, 202, "Inspect registration queue")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	input := retryPassDiscovery(t, f, update)
	require.Equal(t, "forbidden", input.Registration.Reads[0].Error)
	raw, err := json.Marshal(input.Script)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fresh-qa-private-queue")
}
