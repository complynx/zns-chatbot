package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestScriptPassDirectPrivilegedContextRevocation(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment='private-admin-derived-canary' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return registrationRead("queue"), nil
		}
		if calls == 2 {
			require.NotEmpty(t, input.Registration.Reads[0].Queue)
			data, encodeErr := json.Marshal(input.Registration.Reads[0].Queue)
			require.NoError(t, encodeErr)
			require.Contains(t, string(data), "private-admin-derived-canary")
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `tools.preferences.setLanguage({language:"ru"}); return {derived:input};`,
					InputJSON: string(data),
				},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[0].Result), "private-admin-derived-canary")
		return agent.Plan{}, context.Canceled
	})
	update := message(32991, 202, "Inspect private registration queue")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(t.Context(), `ALTER TABLE core.pass_booking_admins RENAME TO unavailable_direct_admins`)
	require.NoError(t, err)
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Empty(t, model.inputs)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='bob' AND update_id=32991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, `"pass_redacted": true`)
	_, err = f.db.Exec(t.Context(), `ALTER TABLE core.unavailable_direct_admins RENAME TO pass_booking_admins`)
	require.NoError(t, err)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "private-admin-derived-canary")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	input = retryPassDiscovery(t, f, update)
	require.Equal(t, "forbidden", input.Registration.Reads[0].Error)
	raw, err := json.Marshal(input.Script)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-admin-derived-canary")
	var operations int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='bob'`).Scan(&operations),
	)
	require.Equal(t, 1, operations)
}
