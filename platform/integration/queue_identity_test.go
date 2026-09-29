package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestIndependentQAQueueIdentityLateDeletion(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	const secret = "independent-queue-removed-resource-canary"
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice'`, secret)
	require.NoError(t, err)
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return registrationRead("queue"), nil
		}
		data, marshalErr := json.Marshal(input.Registration.Reads)
		require.NoError(t, marshalErr)
		require.Contains(t, string(data), secret)
		_, deleteErr := f.db.Exec(
			t.Context(),
			`DELETE FROM core.pass_registration_announcements WHERE owner='alice'; DELETE FROM core.pass_bookings WHERE owner='alice'`,
		)
		require.NoError(t, deleteErr)
		return agent.Plan{View: "workflow", Text: secret}, nil
	})
	err = f.b.Handle(t.Context(), message(881001, 202, "Inspect private queue"))
	require.ErrorContains(
		t,
		err,
		"terminal registration plan",
		"a late answer derived from deleted queue resources must be rejected",
	)
}

func TestIndependentQAQueueIdentityScriptRetry(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	const secret = "independent-script-removed-resource-canary"
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice'`, secret)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return registrationRead("queue"), nil
		}
		if calls == 2 {
			data, marshalErr := json.Marshal(input.Registration.Reads)
			require.NoError(t, marshalErr)
			require.Contains(t, string(data), secret)
			return agent.Plan{
				View:         "workflow",
				ScriptAction: &agent.ScriptProposal{Code: `return input;`, InputJSON: string(data)},
			}, nil
		}
		return agent.Plan{}, context.Canceled
	})
	update := message(881002, 202, "Inspect queue and save result")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.pass_registration_announcements WHERE owner='alice'; DELETE FROM core.pass_bookings WHERE owner='alice'`,
	)
	require.NoError(t, err)
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		data, marshalErr := json.Marshal(input)
		require.NoError(t, marshalErr)
		require.NotContains(
			t,
			string(data),
			secret,
			"retry must not expose deleted queue rows or their script derivatives",
		)
		return agent.Plan{}, context.Canceled
	})
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
}
