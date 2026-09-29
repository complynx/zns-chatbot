package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type invitationQueueVM struct{ beforeInvite func() }

func (vm invitationQueueVM) Evaluate(ctx context.Context, request scriptclient.Request) (json.RawMessage, error) {
	return scopeVM{}.Evaluate(ctx, request)
}

func (vm invitationQueueVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	return scriptworker.Execute(
		ctx,
		scriptprotocol.ExecuteRequest{Code: request.Code, Input: request.Input, Tools: tools},
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			if call.Name == "passes.registration.invite" && vm.beforeInvite != nil {
				vm.beforeInvite()
			}
			return callback(ctx, call)
		},
	)
}

func TestScriptPassQueueInvitationGrounding(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"queue", "explicit", "trusted", "invented", "other_event", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at) VALUES('dance','alice',1,'waitlist','leader','solo','bob',now());
 INSERT INTO core.pass_events(id,finishes_at,titles) VALUES('other',now()+interval '30 days','{"en":"Other"}');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('other',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('other','bob')`,
			)
			require.NoError(t, err)
			code := invitationQueueScript(scenario)
			text := "Invite Alice from the authorized queue"
			update := message(20150, 202, text)
			if scenario == "explicit" {
				update.Message.Text = "Invite 101 from the authorized queue"
			}
			if scenario == "trusted" {
				update.Message.Contact = &telegram.Contact{UserID: 101, FirstName: "Alice"}
			}
			vm := invitationQueueVM{}
			if scenario == "revoked" {
				vm.beforeInvite = func() {
					_, deleteErr := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
					require.NoError(t, deleteErr)
				}
			}
			f.b.Scripts = vm
			model := &knowledgeModel{
				plans: []agent.Plan{
					{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
					{View: "workflow", Text: "Checked"},
				},
			}
			f.b.Model = model
			handle(t, f.b, update)
			require.Len(t, model.inputs, 2)
			run := model.inputs[1].Script.Runs[0]
			event := "dance"
			if scenario == "other_event" {
				event = "other"
			}
			booking, readErr := (passbooking.Service{DB: f.db}).Get(t.Context(), "bob", event)
			require.NoError(t, readErr)
			switch scenario {
			case "queue", "explicit", "trusted":
				require.Empty(t, run.Error)
				assert.EqualValues(t, 101, booking.InvitationTarget)
			case "revoked":
				var omitted struct {
					Omitted bool   `json:"omitted"`
					Reason  string `json:"reason"`
				}
				require.NoError(t, json.Unmarshal(run.Result, &omitted))
				require.True(t, omitted.Omitted)
				require.Equal(t, "pass_access_changed", omitted.Reason)
				var redacted bool
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT (content#>>'{0,pass_redacted}')::boolean FROM bot.interactions WHERE owner='bob' AND update_id=20150 AND kind='script_runs'`).
						Scan(&redacted),
				)
				require.True(t, redacted)
				assert.Zero(t, booking.InvitationTarget)
			default:
				assert.NotEmpty(t, run.Error)
				assert.Zero(t, booking.InvitationTarget)
			}
		})
	}
}

func invitationQueueScript(scenario string) string {
	switch scenario {
	case "trusted", "invented":
		return `await tools.passes.registration.read({event:"dance"});return tools.passes.registration.invite({event:"dance",invite_telegram_id:101});`
	case "other_event":
		return `await tools.passes.admin.queue({event:"dance"});await tools.passes.registration.read({event:"other"});return tools.passes.registration.invite({event:"other",invite_telegram_id:101});`
	default:
		return `const q=await tools.passes.admin.queue({event:"dance"});const target=q.queue.find(b=>b.owner==="alice");return tools.passes.registration.invite({event:"dance",invite_telegram_id:target.telegram_id});`
	}
}
