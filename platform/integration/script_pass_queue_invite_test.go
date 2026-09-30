package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const invitationQueuePrivateCanary = "INVITATION-QUEUE-PRIVATE-SOURCE"

type invitationQueueVM struct {
	beforeInvite func()
	runs         int
	invites      int
	sawPrivate   bool
}

func (vm *invitationQueueVM) Evaluate(ctx context.Context, request scriptclient.Request) (json.RawMessage, error) {
	return scopeVM{}.Evaluate(ctx, request)
}

func (vm *invitationQueueVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	vm.runs++
	return scriptworker.Execute(
		ctx,
		scriptprotocol.ExecuteRequest{Code: request.Code, Input: request.Input, Tools: tools},
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			if call.Name == "passes.registration.invite" {
				vm.invites++
				if vm.beforeInvite != nil {
					vm.beforeInvite()
				}
			}
			raw, err := callback(ctx, call)
			if call.Name == "passes.admin.queue" {
				vm.sawPrivate = strings.Contains(string(raw), invitationQueuePrivateCanary)
			}
			return raw, err
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
				`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,comment) VALUES('dance','alice',1,'waitlist','leader','solo','bob',now(),'INVITATION-QUEUE-PRIVATE-SOURCE');
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
			vm := &invitationQueueVM{}
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
			assertInvitationQueueResult(t, f, model, scenario)
			assertInvitationQueueEffect(t, f, scenario)
			require.Equal(t, 1, vm.runs)
			require.Equal(t, 1, vm.invites, "include denied callback attempts")
			if scenario == "queue" || scenario == "explicit" || scenario == "revoked" {
				require.True(t, vm.sawPrivate, "the retired read actually contained private source text")
			}
			original := invitationQueueSnapshot(t, f)
			calls := len(model.inputs)
			handle(t, f.b, update)
			require.Len(t, model.inputs, calls, "the saved winner cannot replan")
			require.Equal(t, 1, vm.runs)
			require.Equal(t, 1, vm.invites)
			require.JSONEq(
				t,
				original,
				invitationQueueSnapshot(t, f),
				"replay preserves original admission and committed effects",
			)
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

func assertInvitationQueueResult(t *testing.T, f *fixture, model *knowledgeModel, scenario string) {
	t.Helper()
	switch scenario {
	case "queue", "explicit", "revoked":
		require.Len(t, model.inputs, 1, "definitive source retirement forbids another provider exposure")
		assertInvitationQueueRetired(t, f, scenario)
	default:
		require.Len(t, model.inputs, 2)
		require.NotNil(t, model.inputs[1].Script)
		require.Len(t, model.inputs[1].Script.Runs, 1)
		run := model.inputs[1].Script.Runs[0]
		if scenario == "trusted" {
			require.Empty(t, run.Error)
		} else {
			assert.NotEmpty(t, run.Error)
		}
	}
}

func assertInvitationQueueRetired(t *testing.T, f *fixture, scenario string) {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=20150 AND kind='script_runs'`).Scan(&records))
	require.Len(t, records, 1)
	record := records[0]
	if scenario == "revoked" {
		require.Len(t, record.Calls, 1, "revocation rejects invite admission before mutation")
		require.Nil(t, record.Calls[0].Pass)
	}
	require.True(t, record.PassRedacted)
	require.Empty(t, record.Request.Code)
	require.Empty(t, record.Request.InputJSON)
	require.Empty(t, record.Run.Code)
	require.JSONEq(t, `{"omitted":true,"reason":"pass_access_changed"}`, string(record.Run.Result))
	for _, call := range record.Calls {
		require.Empty(t, call.Outcome.Result)
	}
	raw, err := json.Marshal(records)
	require.NoError(t, err)
	require.NotContains(t, string(raw), invitationQueuePrivateCanary)
	var kind, state, notice string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT kind,state,payload->>'system_notice'
 FROM interaction.saved_turns WHERE owner='bob' AND update_id=20150`).Scan(&kind, &state, &notice))
	require.Equal(t, "notice", kind)
	require.Equal(t, "ready", state)
	require.Equal(t, "agent.unavailable", notice)
}

func assertInvitationQueueEffect(t *testing.T, f *fixture, scenario string) {
	t.Helper()
	event := "dance"
	if scenario == "other_event" {
		event = "other"
	}
	booking, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "bob", event)
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='bob'`).Scan(&count),
	)
	switch scenario {
	case "queue", "explicit", "trusted":
		assert.EqualValues(t, 101, booking.InvitationTarget)
		require.Equal(t, "waiting-for-couple", booking.State)
		require.Empty(t, booking.Partner)
		require.Equal(t, 1, count)
		assertInvitationQueueWitness(t, f, scenario)
	default:
		assert.Zero(t, booking.InvitationTarget)
		require.Zero(t, count)
	}
}

func assertInvitationQueueWitness(t *testing.T, f *fixture, scenario string) {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=20150 AND kind='script_runs'`).Scan(&records))
	require.Len(t, records, 1)
	require.Len(t, records[0].Calls, 2)
	request := records[0].Calls[1].Pass
	require.NotNil(t, request)
	require.NotEmpty(t, request.ID)
	require.NotNil(t, request.Command)
	require.NotNil(t, request.Witness)
	command := request.Command
	require.Equal(t, "invite", command.Name)
	require.Equal(t, "dance", command.Event)
	require.Zero(t, command.Version)
	require.EqualValues(t, 101, command.InviteTelegramID)
	require.Equal(t, scenario == "queue", command.QueueInvitation)
	require.Equal(t, "tg-script-20150-0-1", command.Key)
	witness, err := passbooking.CommandOperationWitness("bob", *command)
	require.NoError(t, err)
	require.Equal(t, witness, *request.Witness)
	var keyHash, requestHash string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT key_hash,request_hash FROM core.pass_booking_operations
 WHERE actor='bob' AND event_id='dance'`).Scan(&keyHash, &requestHash))
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(command.Key))), keyHash)
	require.Equal(t, witness.Digest, requestHash)
}

func invitationQueueSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var raw string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'turn',(SELECT to_jsonb(s) FROM interaction.saved_turns s WHERE owner='bob' AND update_id=20150),
 'ledger',(SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=20150 AND kind='script_runs'),
 'operations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY event_id,actor,key_hash) FROM core.pass_booking_operations o),
 'bookings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY event_id,owner) FROM core.pass_bookings b))::text`).Scan(&raw))
	return raw
}
