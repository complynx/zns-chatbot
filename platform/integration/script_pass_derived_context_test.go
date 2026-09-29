package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestScriptPassUntrustedDiscoveryShape(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, transform string }{
		{"null-page", `jsonb_set(content,'{0,calls,0,outcome,result}','null'::jsonb)`},
		{"null-items", `jsonb_set(content,'{0,calls,0,outcome,result,items}','null'::jsonb)`},
		{"missing-items", `content #- '{0,calls,0,outcome,result,items}'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := archivedPassFixture(t)
			update := pausePassDiscovery(t, f, `return tools.passes.events({});`)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE bot.interactions SET content=`+tc.transform+` WHERE owner='alice' AND update_id=29991 AND kind='script_runs'`,
			)
			require.NoError(t, err)
			input := retryPassDiscovery(t, f, update)
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
			require.Equal(t, 1, input.Script.Remaining)
		})
	}
}

func TestScriptPassCurrentBookingExpiresBeforeRemoval(t *testing.T) {
	t.Parallel()
	for _, code := range []string{`return tools.passes.get({event:"archive"});`, `return tools.passes.registration.read({event:"archive",view:"home"});`} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			f := archivedPassFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'; UPDATE core.pass_bookings SET comment='fresh-expired-secret' WHERE event_id='archive'`,
			)
			require.NoError(t, err)
			update := pausePassDiscovery(t, f, code)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_events SET finishes_at=now()-interval '1 year' WHERE id='archive'`,
			)
			require.NoError(t, err)
			removeArchivedBooking(t, f)
			input := retryPassDiscovery(t, f, update)
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "fresh-expired-secret")
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
		})
	}
}

func TestScriptPassDirectReadDerivedRevocation(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment='fresh-direct-secret' WHERE event_id='archive'`,
	)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{
				View: agent.RegistrationView,
				RegistrationAction: &agent.RegistrationProposal{
					Name:  agent.RegistrationRead,
					Event: "archive",
					View:  "home",
				},
			}, nil
		}
		if calls == 2 {
			require.Equal(t, "fresh-direct-secret", input.Registration.Reads[0].Booking.Comment)
			data, encodeErr := json.Marshal(input.Registration.Reads[0].Booking.Comment)
			require.NoError(t, encodeErr)
			return agent.Plan{
				View:         "workflow",
				ScriptAction: &agent.ScriptProposal{Code: `return {derived:input};`, InputJSON: string(data)},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[0].Result), "fresh-direct-secret")
		return agent.Plan{}, context.Canceled
	})
	update := message(30991, 101, "Read my historical registration")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	removeArchivedBooking(t, f)
	input := retryPassDiscovery(t, f, update)
	require.Equal(t, "forbidden", input.Registration.Reads[0].Error)
	raw, err := json.Marshal(input.Script)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fresh-direct-secret")
}
