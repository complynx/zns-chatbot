package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestPassPlanLateModelRevocation(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	const secret = "MEMBERSHIP_ONLY_LATE_SECRET"
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE event_id='archive'`, secret)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `return tools.passes.get({event:"archive"});`,
					InputJSON: "null",
				},
			}, nil
		}
		encoded, marshalErr := json.Marshal(input.Script)
		require.NoError(t, marshalErr)
		require.Contains(t, string(encoded), secret)
		// The response is withheld until current owner membership is removed.
		removeArchivedBooking(t, f)
		return agent.Plan{View: "workflow", Text: secret}, nil
	})
	err = f.b.Handle(t.Context(), message(46991, 101, "Read my private archived pass"))
	if err == nil {
		require.NotContains(t, strings.ReplaceAll(workflowCard(t, f).Text, `\`, ""), secret)
	}
	var saved string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((SELECT payload::text FROM interaction.saved_turns WHERE owner='alice' AND update_id=46991),'')`).
			Scan(&saved),
	)
	require.NotContains(t, saved, secret)
}
