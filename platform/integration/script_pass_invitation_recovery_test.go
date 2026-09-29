package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const invitationIdentityCanary = "private-invitation-incarnation"

func invitationIdentityFixture(t *testing.T) (*fixture, passbooking.Booking) {
	t.Helper()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET name=$1 WHERE id='alice'`, invitationIdentityCanary)
	require.NoError(t, err)
	command := bookingCommand("invite", "identity-invite", passbooking.Booking{})
	command.InviteTelegramID = 202
	first, err := (passbooking.Service{DB: f.db}).Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	return f, first
}

func pauseDerivedInvitation(t *testing.T, f *fixture) telegram.Update {
	t.Helper()
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{
				View: agent.RegistrationView,
				RegistrationAction: &agent.RegistrationProposal{
					Name:  agent.RegistrationRead,
					Event: "dance",
					View:  "invitations",
				},
			}, nil
		}
		if calls == 2 {
			data, err := json.Marshal(input.Registration.Reads)
			require.NoError(t, err)
			require.Contains(t, string(data), invitationIdentityCanary)
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `tools.preferences.setLanguage({language:"ru"}); return input;`,
					InputJSON: string(data),
				},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[0].Result), invitationIdentityCanary)
		return agent.Plan{}, context.Canceled
	})
	update := message(45991, 202, "Read my invitations for dance")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	return update
}

func TestScriptPassDerivedInvitationIncarnation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"replacement", "legacy dependency", "outage"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f, first := invitationIdentityFixture(t)
			update := pauseDerivedInvitation(t, f)
			if scenario == "outage" {
				checkInvitationOutage(t, f, update)
			} else {
				invalidateInvitationIdentity(t, f, first, scenario)
			}
			f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Scripts: scopeVM{}}
			input := retryPassDiscovery(t, f, update)
			if scenario == "outage" {
				require.Contains(t, string(input.Script.Runs[0].Result), invitationIdentityCanary)
			} else {
				require.NotContains(t, string(input.Script.Runs[0].Result), invitationIdentityCanary)
				require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
			}
			require.Equal(t, 1, input.Script.Remaining)
			var operations int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='bob'`).
					Scan(&operations),
			)
			require.Equal(t, 1, operations)
		})
	}
}

func checkInvitationOutage(t *testing.T, f *fixture, update telegram.Update) {
	t.Helper()
	transport := &passToolAuthorityTransport{}
	transport.fail.Store(true)
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Empty(t, model.inputs)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='bob' AND update_id=45991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, `"pass_redacted": true`)
	transport.fail.Store(false)
}

func invalidateInvitationIdentity(t *testing.T, f *fixture, first passbooking.Booking, scenario string) {
	t.Helper()
	if scenario == "legacy dependency" {
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE bot.interactions SET content=content #- '{0,pass_context,0,invitations,0,created_at}' WHERE owner='bob' AND update_id=45991 AND kind='script_runs'`,
		)
		require.NoError(t, err)
		return
	}
	service := passbooking.Service{DB: f.db}
	_, err := service.Execute(t.Context(), "alice", bookingCommand("cancel", "identity-withdraw", first))
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.pass_registration_announcements WHERE event_id='dance' AND owner='alice'; DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'`,
	)
	require.NoError(t, err)
	command := bookingCommand("invite", "identity-replacement", passbooking.Booking{})
	command.InviteTelegramID = 202
	second, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Equal(t, first.Version, second.Version)
	require.NotEqual(t, first.CreatedAt, second.CreatedAt)
}

func TestScriptPassLegacyInvitationResult(t *testing.T) {
	t.Parallel()
	for _, item := range []struct{ code, path string }{
		{`return tools.passes.invitations({event:"dance"});`, `{0,calls,0,outcome,result,items,0,created_at}`},
		{`return tools.passes.registration.read({event:"dance",view:"invitations"});`, `{0,calls,0,outcome,result,invitations,0,created_at}`},
	} {
		t.Run(item.code, func(t *testing.T) {
			t.Parallel()
			f, _ := invitationIdentityFixture(t)
			update := pausePrivatePassTool(t, f, item.code, invitationIdentityCanary)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE bot.interactions SET content=content #- $1::text[] WHERE owner='bob' AND update_id=43991 AND kind='script_runs'`,
				item.path,
			)
			require.NoError(t, err)
			removeLegacyPassToolAuthority(t, f, "bob", 43991, 0)
			input := retryPassDiscovery(t, f, update)
			require.NotContains(t, string(input.Script.Runs[0].Result), invitationIdentityCanary)
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
		})
	}
}
