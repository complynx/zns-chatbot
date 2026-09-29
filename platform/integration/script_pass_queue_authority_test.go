package integration_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type qaInviteInterruption struct{ stopped bool }

func (tr *qaInviteInterruption) RoundTrip(req *http.Request) (*http.Response, error) {
	if (req.URL.Path == "/v1/passes/actions" || req.URL.Path == "/internal/derived/pass-actions") && !tr.stopped {
		tr.stopped = true
		return nil, errors.New("QA interruption before dispatch")
	}
	return http.DefaultTransport.RoundTrip(req)
}
func TestScriptPassQueueAuthorityResumeAfterRevocation(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at) VALUES('dance','alice',1,'waitlist','leader','solo','bob',now())`,
	)
	require.NoError(t, err)
	f.b.API.HTTP = &http.Client{Transport: &qaInviteInterruption{}}
	f.b.Host.HTTP = f.b.API.HTTP
	r := runPassVM(
		t,
		f,
		29801,
		202,
		"Invite Alice from the authorized queue",
		`const q=await tools.passes.admin.queue({event:"dance"});return tools.passes.registration.invite({event:"dance",invite_telegram_id:q.queue.find(b=>b.owner==="alice").telegram_id});`,
	)
	require.Empty(t, r.Error)
	var pending struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &pending))
	require.NotEmpty(t, pending.ID)
	require.False(t, pending.Complete)
	before, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Zero(t, before.InvitationTarget)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	retry := runPassVM(
		t,
		f,
		29802,
		202,
		"Resume the operation",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, pending.ID),
	)
	after, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	t.Logf("resume error=%q result=%s target=%d", retry.Error, retry.Result, after.InvitationTarget)
	assert.Zero(t, after.InvitationTarget, "queue-only authority was revoked before any invitation effect")
}
func TestScriptPassQueueAuthorityExecutionRevocation(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at) VALUES('dance','alice',1,'waitlist','leader','solo','bob',now())`,
	)
	require.NoError(t, err)
	f.b.API.HTTP = &http.Client{Transport: &passActionTransport{before: func() {
		_, e := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
		require.NoError(t, e)
	}}}
	f.b.Host.HTTP = f.b.API.HTTP
	r := runPassVM(
		t,
		f,
		29803,
		202,
		"Invite Alice from the authorized queue",
		`const q=await tools.passes.admin.queue({event:"dance"});return tools.passes.registration.invite({event:"dance",invite_telegram_id:q.queue.find(b=>b.owner==="alice").telegram_id});`,
	)
	after, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	t.Logf("error=%q result=%s target=%d", r.Error, r.Result, after.InvitationTarget)
	assert.Zero(t, after.InvitationTarget, "queue grant revoked between reservation and execution")
}

func TestScriptPassOrdinaryInvitationIgnoresQueueGrant(t *testing.T) {
	t.Parallel()
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted_%t", trusted), func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			f.b.API.HTTP = &http.Client{Transport: &passActionTransport{before: func() {
				_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
				require.NoError(t, err)
			}}}
			f.b.Host.HTTP = f.b.API.HTTP
			code := `await tools.passes.registration.read({event:"dance"});return tools.passes.registration.invite({event:"dance",invite_telegram_id:101});`
			f.b.Scripts = scopeVM{}
			model := &knowledgeModel{
				plans: []agent.Plan{
					{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
					{View: "workflow", Text: "Checked"},
				},
			}
			f.b.Model = model
			update := message(29810, 202, "Invite 101")
			if trusted {
				update.Message.Text = "Invite this trusted contact"
				update.Message.Contact = &telegram.Contact{UserID: 101, FirstName: "Alice"}
			}
			handle(t, f.b, update)
			require.Len(t, model.inputs, 2)
			result := model.inputs[1].Script.Runs[0]
			require.Empty(t, result.Error)
			var completed struct {
				ID       string `json:"operation_id"`
				Complete bool   `json:"complete"`
			}
			require.NoError(t, json.Unmarshal(result.Result, &completed))
			require.True(t, completed.Complete)
			replay := runPassVM(
				t,
				f,
				29811,
				202,
				"Resume my operation",
				fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, completed.ID),
			)
			require.Empty(t, replay.Error)
			booking, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "bob", "dance")
			require.NoError(t, err)
			assert.EqualValues(t, 101, booking.InvitationTarget)
			var provenance bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT content::text LIKE '%queue_invitation%' FROM bot.interactions WHERE owner='bob' AND update_id=29810 AND kind='script_runs'`).
					Scan(&provenance),
			)
			assert.False(t, provenance, "ordinary provenance remains omitted from durable legacy-compatible JSON")
		})
	}
}
