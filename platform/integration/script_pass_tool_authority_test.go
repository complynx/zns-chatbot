package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func pausePrivatePassTool(t *testing.T, f *fixture, code, needle string) telegram.Update {
	t.Helper()
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}}, nil
		}
		require.Empty(t, input.Script.Runs[0].Error)
		require.Contains(t, string(input.Script.Runs[0].Result), needle)
		return agent.Plan{}, context.Canceled
	})
	update := message(43991, 202, "Inspect registration for event dance and Telegram ID 101")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	return update
}

func TestScriptPassToolAuthorityClasses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, code, needle, revoke string }{
		{"queue", `return tools.passes.admin.queue({event:"dance"});`, "tool-private-canary", `DELETE FROM core.pass_booking_admins WHERE owner='bob'`},
		{"target", `return tools.passes.admin.target({event:"dance",target:"101"});`, "tool-private-canary", `DELETE FROM core.pass_booking_admins WHERE owner='bob'`},
		{"payment review", `return tools.passes.payments.review({event:"dance"});`, "payment_queue", `DELETE FROM core.pass_payment_admins WHERE owner='bob'`},
		{"takeover", `return tools.passes.takeover.read({event:"dance",target:"101"});`, "tool-private-canary", `DELETE FROM core.pass_booking_admins WHERE owner='bob'; DELETE FROM core.pass_payment_admins WHERE owner='bob'`},
		{"tiers", `return tools.passes.tiers({event:"dance"});`, "full_balance", `DELETE FROM core.pass_booking_admins WHERE owner='bob'; DELETE FROM core.pass_payment_admins WHERE owner='bob'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET comment='tool-private-canary' WHERE owner='alice'`,
			)
			require.NoError(t, err)
			needle := test.needle
			if test.name == "payment review" {
				photo, _ := intakePhoto(t, f)
				f.model.plan = agent.Plan{
					View: agent.MediaView,
					MediaAction: &agent.MediaProposal{
						MediaID:  "tg-media-100",
						Intent:   "receipt",
						Amount:   "100",
						Currency: "RUB",
					},
				}
				handle(t, f.b, photo)
				payment, paymentErr := (passbooking.Service{DB: f.db}).Payment(t.Context(), "alice", "dance", "alice")
				require.NoError(t, paymentErr)
				require.NotEmpty(t, payment.ProofID)
				needle = payment.ProofID
			}
			update := pausePrivatePassTool(t, f, `tools.preferences.setLanguage({language:"ru"}); `+test.code, needle)
			_, err = f.db.Exec(t.Context(), test.revoke)
			require.NoError(t, err)
			input := retryPassDiscovery(t, f, update)
			raw, err := json.Marshal(input.Script)
			require.NoError(t, err)
			require.NotContains(t, string(raw), needle)
			require.Contains(t, string(raw), "pass_access_changed")
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

func TestScriptPassInvitationContextWithdrawal(t *testing.T) {
	t.Parallel()
	for _, code := range []string{`return tools.passes.invitations({event:"dance"});`, `return tools.passes.registration.read({event:"dance",view:"invitations"});`} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET name='private-inviter-canary' WHERE id='alice'`)
			require.NoError(t, err)
			command := bookingCommand("invite", "context-invitation", passbooking.Booking{})
			command.InviteTelegramID = 202
			service := passbooking.Service{DB: f.db}
			booking, err := service.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			update := pausePrivatePassTool(t, f, code, "private-inviter-canary")
			_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
			require.NoError(t, err)
			input := retryPassDiscovery(t, f, update)
			require.Contains(
				t,
				string(input.Script.Runs[0].Result),
				"private-inviter-canary",
				"invitation ownership does not require admin rights",
			)
			_, err = service.Execute(
				t.Context(),
				"alice",
				bookingCommand("cancel", "withdraw-context-invitation", booking),
			)
			require.NoError(t, err)
			input = retryPassDiscovery(t, f, update)
			require.NotContains(t, string(input.Script.Runs[0].Result), "private-inviter-canary")
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
		})
	}
}

func TestScriptPassCommittedBatchStatusPreserved(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Test Name'`)
	require.NoError(t, err)
	update := pausePrivatePassTool(
		t,
		f,
		`return tools.passes.batch.assign({event:"dance",recipients:[101],assignment:{create:true,from_profile:true,comment:"committed-private-canary"}});`,
		"succeeded",
	)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "succeeded")
	var batches int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_batches`).Scan(&batches))
	require.Equal(t, 1, batches)
	result := runPassVM(t, f, 43992, 202, "List my operation references", `return tools.passes.operations({});`)
	require.Empty(t, result.Error)
	require.JSONEq(t, `[]`, string(result.Result), "revoked privileged operations are not discoverable")
	foreign := runPassVM(t, f, 43993, 101, "List my operation references", `return tools.passes.operations({});`)
	require.Empty(t, foreign.Error)
	require.JSONEq(t, `[]`, string(foreign.Result))
}
