package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type passToolAuthorityTransport struct {
	fail atomic.Bool
	hits atomic.Int64
}

func (transport *passToolAuthorityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if (strings.HasPrefix(request.URL.Path, "/v1/passes/") || request.Method == http.MethodPost && request.URL.Path == "/internal/history/authority") &&
		transport.fail.Load() {
		transport.hits.Add(1)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"synthetic_unavailable"}`)),
			Request:    request,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestScriptPassDirectInvitationContext(t *testing.T) {
	t.Parallel()
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "withdrawn", true: "unknown legacy"}[legacy], func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET name='direct-inviter-canary' WHERE id='alice'`)
			require.NoError(t, err)
			service := passbooking.Service{DB: f.db}
			command := bookingCommand("invite", "direct-tool-invitation", passbooking.Booking{})
			command.InviteTelegramID = 202
			booking, err := service.Execute(t.Context(), "alice", command)
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
							Event: "dance",
							View:  "invitations",
						},
					}, nil
				}
				if calls == 2 {
					data, marshalErr := json.Marshal(input.Registration.Reads)
					require.NoError(t, marshalErr)
					require.Contains(t, string(data), "direct-inviter-canary")
					return agent.Plan{
						View:         "workflow",
						ScriptAction: &agent.ScriptProposal{Code: `return input;`, InputJSON: string(data)},
					}, nil
				}
				require.Contains(t, string(input.Script.Runs[0].Result), "direct-inviter-canary")
				return agent.Plan{}, context.Canceled
			})
			update := message(43995, 202, "Read my invitations for dance")
			require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
			if legacy {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE bot.interactions SET content=content #- '{0,pass_context,0,invitations}' WHERE owner='bob' AND update_id=43995 AND kind='script_runs'`,
				)
			} else {
				_, err = service.Execute(
					t.Context(),
					"alice",
					bookingCommand("cancel", "withdraw-direct-tool-invitation", booking),
				)
			}
			require.NoError(t, err)
			input := retryPassDiscovery(t, f, update)
			require.NotContains(t, string(input.Script.Runs[0].Result), "direct-inviter-canary")
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
		})
	}
}

func TestScriptPassToolContextOutage(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	update := pausePrivatePassTool(t, f, `return tools.passes.tiers({event:"dance"});`, "full_balance")
	transport := &passToolAuthorityTransport{}
	transport.fail.Store(true)
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Positive(t, transport.hits.Load())
	require.Empty(t, model.inputs)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='bob' AND update_id=43991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, `"pass_redacted": true`)
	transport.fail.Store(false)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "full_balance")
}

func TestScriptPassCommittedAssignmentPrivateResult(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Test Name'`)
	require.NoError(t, err)
	update := pausePrivatePassTool(t, f, `const target=tools.passes.admin.target({event:"dance",target:"101"});
return tools.passes.admin.assign({event:"dance",target:target.admin_target.booking.owner,assignment:{create:true,from_profile:true,total_price:150}});`, `"assigned"`)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	input := retryPassDiscovery(t, f, update)
	require.NotContains(t, string(input.Script.Runs[0].Result), `"assigned"`)
	require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
	var assignments int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&assignments),
	)
	require.Equal(t, 1, assignments)
}

func TestScriptPassLegacyTierReferenceFailsClosed(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	update := pausePrivatePassTool(t, f, `return tools.passes.tiers({event:"dance"});`, "full_balance")
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE bot.interactions SET content=content #- '{0,calls,0,pass_read}' WHERE owner='bob' AND update_id=43991 AND kind='script_runs'`,
	)
	require.NoError(t, err)
	removeLegacyPassToolAuthority(t, f, "bob", 43991, 0)
	input := retryPassDiscovery(t, f, update)
	require.NotContains(t, string(input.Script.Runs[0].Result), "full_balance")
	require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
}

// Legacy fixtures must remove the modern authority envelope as well as the old field.
func removeLegacyPassToolAuthority(t *testing.T, f *fixture, owner string, update int64, index int) {
	t.Helper()
	source := []string{"0", "calls", strconv.Itoa(index), "source"}
	result := []string{"0", "calls", strconv.Itoa(index), "result_authorities"}
	tag, err := f.db.Exec(
		t.Context(),
		`UPDATE bot.interactions SET content=content #- $3::text[] #- $4::text[] WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`,
		owner,
		update,
		source,
		result,
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	var absent bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content #> $3::text[] IS NULL AND content #> $4::text[] IS NULL FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`, owner, update, source, result).
			Scan(&absent),
	)
	require.True(t, absent)
}
