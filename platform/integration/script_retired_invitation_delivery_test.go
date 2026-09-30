package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const r96PrivateQueue = "R96-PRIVATE-QUEUE-BODY"
const r96Update int64 = 29650

func TestRetiredInvitationReceiptDelivered(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, scenario := range []string{"queue", "explicit", "revoked", "invented", "other_event"} {
			t.Run(language+"/"+scenario, func(t *testing.T) {
				t.Parallel()
				f, model, vm, update := r96InvitationFixture(t, language, scenario)
				handle(t, f.b, update)
				committed := scenario == "queue" || scenario == "explicit"
				before := r96InvitationState(t, f, committed)
				if committed {
					require.Len(
						t,
						model.inputs,
						1,
						"retired VM must not resume a model to explain the committed effect",
					)
				}
				r96DrainInvitation(t, f)
				r96InvitationWire(t, f, language, committed, before)
				require.Equal(t, before, r96InvitationState(t, f, committed))
				r96ReplayInvitation(t, f, model, vm, update, before, committed)
			})
		}
	}
}

func TestRetiredInvitationReceiptDeliveryCurrentRights(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, scenario := range []string{"queue", "explicit"} {
			t.Run(language+"/"+scenario, func(t *testing.T) {
				t.Parallel()
				f, model, vm, update := r96InvitationFixture(t, language, scenario)
				handle(t, f.b, update)
				require.Len(t, model.inputs, 1)
				committed := true
				before := r96InvitationState(t, f, committed)
				var queued r96ReceiptCardState
				if scenario == "queue" {
					queued = r96QueuedReceiptCard(t, f)
				}
				_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
				require.NoError(t, err)
				// Reconstruct before dispatch: queued status must recheck today's exact command rights.
				f.b = r96Reconstruct(f.b)
				r96DrainInvitation(t, f)
				r96InvitationWire(t, f, language, scenario == "explicit", before)
				require.Equal(t, before, r96InvitationState(t, f, true))
				if scenario == "queue" {
					r96ReplayCancelledInvitation(t, f, model, vm, update, before, queued, language)
					return
				}
				r96ReplayInvitation(t, f, model, vm, update, before, committed)
			})
		}
	}
}

type r96InvitationVM struct {
	scopeVM

	beforeInvite func()
	runs         int
	invites      int
	sawPrivate   bool
}

func (vm *r96InvitationVM) Execute(ctx context.Context, request scriptclient.Request, tools []scriptclient.Tool,
	callback scriptclient.Callback) (json.RawMessage, error) {
	vm.runs++
	return vm.scopeVM.Execute(
		ctx,
		request,
		tools,
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			if call.Name == "passes.registration.invite" {
				vm.invites++
				if vm.beforeInvite != nil {
					vm.beforeInvite()
				}
			}
			raw, err := callback(ctx, call)
			if call.Name == "passes.admin.queue" {
				vm.sawPrivate = strings.Contains(string(raw), r96PrivateQueue)
			}
			return raw, err
		},
	)
}

func r96InvitationFixture(
	t *testing.T,
	language, scenario string,
) (*fixture, *knowledgeModel, *r96InvitationVM, telegram.Update) {
	t.Helper()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_bookings
 (event_id,owner,version,state,role,kind,payment_admin,created_at,comment)
 VALUES('dance','alice',1,'waitlist','leader','solo','bob',now(),$1)`, r96PrivateQueue)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at,titles)
 VALUES('other',now()+interval '30 days','{"en":"Other","ru":"Другое"}');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('other',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('other','bob')`)
	require.NoError(t, err)
	vm := &r96InvitationVM{}
	if scenario == "revoked" {
		vm.beforeInvite = func() {
			_, deleteErr := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
			require.NoError(t, deleteErr)
		}
	}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View:         "workflow",
			ScriptAction: &agent.ScriptProposal{Code: invitationQueueScript(scenario), InputJSON: "null"},
		},
		{View: "workflow", Text: "No confirmed invitation."},
	}}
	f.b.Model, f.b.Scripts = model, vm
	text := "Invite Alice from the authorized queue"
	if scenario == "explicit" {
		text = "Invite 101 from the authorized queue"
	}
	update := message(r96Update, 202, text)
	update.Message.From.LanguageCode = language
	return f, model, vm, update
}

type r96InvitationSnapshot struct {
	Saved      string
	Effects    string
	Admissions []agenthost.ScriptPassRequest
}

func r96InvitationState(t *testing.T, f *fixture, committed bool) r96InvitationSnapshot {
	t.Helper()
	var result r96InvitationSnapshot
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT to_jsonb(s)::text FROM interaction.saved_turns s
 WHERE owner='bob' AND update_id=$1`, r96Update).Scan(&result.Saved))
	var saved struct {
		Kind    string `json:"kind"`
		State   string `json:"state"`
		Payload struct {
			SystemNotice string `json:"system_notice"`
		} `json:"payload"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Saved), &saved))
	if committed {
		require.Equal(t, "notice", saved.Kind)
		require.Equal(t, "ready", saved.State)
		require.Equal(t, "agent.unavailable", saved.Payload.SystemNotice)
	}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'bookings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY event_id,owner) FROM core.pass_bookings b),
 'operations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY event_id,actor,key_hash) FROM core.pass_booking_operations o))::text`).Scan(&result.Effects))
	var count, target int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='bob'`).Scan(&count),
	)
	expected := int64(0)
	if committed {
		expected = 1
	}
	require.Equal(t, expected, count)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT COALESCE((SELECT invitation_target FROM core.pass_bookings
 WHERE owner='bob' AND event_id='dance'),0)`).Scan(&target))
	if committed {
		require.EqualValues(t, 101, target)
	} else {
		require.Zero(t, target)
	}
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, r96Update).Scan(&records))
	for _, record := range records {
		for _, call := range record.Calls {
			if call.Pass != nil {
				result.Admissions = append(result.Admissions, *call.Pass)
			}
		}
	}
	if committed {
		require.Len(t, result.Admissions, 1)
		require.NotEmpty(t, result.Admissions[0].ID)
		require.NotNil(t, result.Admissions[0].Witness)
		require.NotNil(t, result.Admissions[0].Command)
		command := result.Admissions[0].Command
		require.Equal(t, "invite", command.Name)
		require.Equal(t, "dance", command.Event)
		require.Equal(t, "tg-script-29650-0-1", command.Key)
		require.EqualValues(t, 101, command.InviteTelegramID)
		witness, err := passbooking.CommandOperationWitness("bob", *command)
		require.NoError(t, err)
		require.Equal(t, witness, *result.Admissions[0].Witness)
		var keyHash, requestHash string
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT key_hash,request_hash FROM core.pass_booking_operations
 WHERE actor='bob' AND event_id='dance'`).Scan(&keyHash, &requestHash))
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(command.Key))), keyHash)
		require.Equal(t, witness.Digest, requestHash)
	}
	return result
}

func r96InvitationWire(t *testing.T, f *fixture, language string, status bool, original r96InvitationSnapshot) {
	t.Helper()
	var deliveryState string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object(
 'operation',operation_key,'effect',effect_key,'state',state,'reason',reason,
 'attempt',attempt,'message_id',message_id,'reference',reference)
 ORDER BY operation_key,effect_key),'[]'::jsonb)::text FROM bot.delivery_intents WHERE owner='bob'`).Scan(&deliveryState))
	t.Logf("R96_DELIVERY_STATE %s", deliveryState)
	var wire struct {
		Messages []telegram.Message `json:"Messages"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((SELECT data FROM bot.fake_state WHERE id=true), '{"Messages":[]}'::jsonb)`).
			Scan(&wire),
	)
	var workflowMessage int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((SELECT message_id FROM bot.messages WHERE owner='bob'),0)`).
			Scan(&workflowMessage),
	)
	if status {
		require.Positive(t, workflowMessage, "committed status must reach the actual workflow card")
	}
	var texts []string
	for _, item := range wire.Messages {
		if item.Chat.ID == 202 && item.ID == workflowMessage {
			texts = append(texts, strings.ReplaceAll(item.Text, `\`, ""))
		}
	}
	// A cancelled stale receipt may have no transport output; a positive status must reach Telegram.
	all := strings.Join(texts, "\n")
	invite, caution := "Invitation — committed.", "Other script steps are not confirmed."
	if language == "ru" {
		invite, caution = "Приглашение — сохранено.", "Остальные шаги сценария не подтверждены."
	}
	if status {
		require.Contains(t, all, invite)
		require.Contains(t, all, caution)
	} else {
		require.NotContains(t, all, invite)
	}
	for _, forbidden := range []string{r96PrivateQueue, "101", "tg-script-", "alice", "Alice", "Invitation accepted", "Приглашение принято", "All steps completed", "Все шаги выполнены"} {
		require.NotContains(t, all, forbidden)
	}
	for _, admission := range original.Admissions {
		require.NotContains(t, all, admission.ID)
	}
}

func r96Reconstruct(previous *bot.Bot) *bot.Bot {
	return &bot.Bot{DB: previous.DB, API: previous.API, Host: previous.Host, TG: previous.TG,
		Delivery: previous.Delivery, Model: previous.Model, Scripts: previous.Scripts}
}

func r96ReplayInvitation(t *testing.T, f *fixture, model *knowledgeModel, vm *r96InvitationVM,
	update telegram.Update, original r96InvitationSnapshot, committed bool) {
	t.Helper()
	require.Equal(t, 1, vm.runs)
	require.Equal(t, 1, vm.invites, "count even rejected callback attempts")
	if len(original.Admissions) > 0 && original.Admissions[0].Command != nil &&
		original.Admissions[0].Command.QueueInvitation {
		require.True(t, vm.sawPrivate)
	}
	calls := len(model.inputs)
	var before, after string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((SELECT data FROM bot.fake_state WHERE id=true), '{"Messages":[]}'::jsonb)::text`).
			Scan(&before),
	)
	f.b = r96Reconstruct(f.b)
	handlePassVisible(t, f, update)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((SELECT data FROM bot.fake_state WHERE id=true), '{"Messages":[]}'::jsonb)::text`).
			Scan(&after),
	)
	require.JSONEq(t, before, after, "reconstruction and input replay cannot duplicate Telegram sends or edits")
	require.Len(t, model.inputs, calls)
	require.Equal(t, 1, vm.runs)
	require.Equal(t, 1, vm.invites)
	require.Equal(t, original, r96InvitationState(t, f, committed))
}

type r96ReceiptCardState struct {
	Intent       botdelivery.Intent
	IntentJSON   string
	QueueJSON    string
	QueueState   string
	QueueChat    string
	Thread       int64
	LaneSequence int64
}

func r96ReceiptCard(t *testing.T, f *fixture, operation string) r96ReceiptCardState {
	t.Helper()
	var state r96ReceiptCardState
	var err error
	state.Intent, err = botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: "view"}, false)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT to_jsonb(i)::text,to_jsonb(q)::text,
q.state,q.chat,q.thread_id,q.lane_sequence FROM bot.delivery_intents i JOIN core.delivery_queue q
ON q.bot_id=i.bot_id AND q.owner_kind='bot' AND q.owner_key=i.operation_key AND q.effect_key=i.effect_key
WHERE i.bot_id=$1 AND i.operation_key=$2 AND i.effect_key='view'`, f.b.Delivery.BotID, operation).
		Scan(&state.IntentJSON, &state.QueueJSON, &state.QueueState, &state.QueueChat, &state.Thread, &state.LaneSequence),
	)
	return state
}

func r96QueuedReceiptCard(t *testing.T, f *fixture) r96ReceiptCardState {
	t.Helper()
	var operations []string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT array_agg(operation_key ORDER BY operation_key)
FROM bot.delivery_intents WHERE bot_id=$1 AND owner='bob' AND reference->>'object' LIKE 'registration-receipts:%'
AND (reference->>'update')::bigint=$2`, f.b.Delivery.BotID, r96Update).Scan(&operations))
	require.Len(t, operations, 1, "pin the exact enriched receipt before revocation")
	state := r96ReceiptCard(t, f, operations[0])
	require.Equal(t, delivery.Deferred, state.Intent.State)
	require.Equal(t, "pending", state.QueueState)
	require.Equal(t, "bob", state.Intent.Owner)
	require.EqualValues(t, 202, state.Intent.Chat)
	require.Equal(t, "202", state.QueueChat)
	require.Zero(t, state.Thread)
	require.Zero(t, state.Intent.Attempt)
	require.Zero(t, state.Intent.MessageID)
	require.False(t, state.Intent.ContinuationDone)
	require.NotEmpty(t, state.Intent.Reference.Authorities)
	return state
}

func r96CancelledReceiptCard(t *testing.T, f *fixture, queued r96ReceiptCardState) r96ReceiptCardState {
	t.Helper()
	state := r96ReceiptCard(t, f, queued.Intent.Operation)
	require.Equal(t, delivery.Cancelled, state.Intent.State)
	require.Equal(t, "cancelled", state.QueueState)
	require.Equal(t, queued.Intent.Reference, state.Intent.Reference)
	require.Equal(t, queued.Intent.QueueReference(), state.Intent.QueueReference())
	require.Equal(t, queued.Intent.Owner, state.Intent.Owner)
	require.Equal(t, queued.Intent.Chat, state.Intent.Chat)
	require.Equal(t, queued.Intent.Target, state.Intent.Target)
	require.Equal(t, queued.Intent.Phase, state.Intent.Phase)
	require.Equal(t, queued.Intent.Receipt, state.Intent.Receipt)
	require.Equal(t, queued.QueueChat, state.QueueChat)
	require.Equal(t, queued.Thread, state.Thread)
	require.Equal(t, queued.LaneSequence, state.LaneSequence)
	require.Zero(t, state.Intent.Attempt)
	require.Zero(t, state.Intent.MessageID)
	require.False(t, state.Intent.ContinuationDone)
	var reason string
	var projected int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT reason FROM bot.delivery_intents
WHERE bot_id=$1 AND operation_key=$2 AND effect_key='view'`, f.b.Delivery.BotID, state.Intent.Operation).Scan(&reason))
	require.Equal(t, "source_unavailable", reason)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.messages WHERE owner='bob'`).Scan(&projected),
	)
	require.Zero(t, projected, "pre-send cancellation cannot manufacture a successful workflow projection")
	return state
}

func r96ReplayCancelledInvitation(t *testing.T, f *fixture, model *knowledgeModel, vm *r96InvitationVM,
	update telegram.Update, original r96InvitationSnapshot, queued r96ReceiptCardState, language string) {
	t.Helper()
	cancelled := r96CancelledReceiptCard(t, f, queued)
	calls := len(model.inputs)
	before := r96FullWire(t, f)
	f.b = r96Reconstruct(f.b)
	handlePassVisible(t, f, update)
	added := r96OneFallbackWire(t, before, r96FullWire(t, f))
	r96ExactGenericFallback(t, f, language, added)
	r96InvitationWire(t, f, language, false, original)
	require.Equal(t, cancelled, r96ReceiptCard(t, f, cancelled.Intent.Operation))
	completed := r96CompletedFallback(t, f, cancelled, added)
	require.Len(t, model.inputs, calls)
	require.Equal(t, 1, vm.runs)
	require.Equal(t, 1, vm.invites)
	require.Equal(t, original, r96InvitationState(t, f, true))
	completedWire := r96FullWire(t, f)
	// Once this first safe fallback actually completed, the original full-wire
	// replay assertion applies without any exception or filtered message list.
	r96ReplayInvitation(t, f, model, vm, update, original, true)
	require.Equal(t, completedWire, r96FullWire(t, f), "completed fallback replay preserves the full wire bytes")
	require.Equal(t, cancelled, r96ReceiptCard(t, f, cancelled.Intent.Operation))
	require.Equal(t, completed, r96ReceiptCard(t, f, completed.Intent.Operation))
}

func r96FullWire(t *testing.T, f *fixture) string {
	t.Helper()
	var raw string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT data::text FROM bot.fake_state WHERE id=true`).Scan(&raw))
	return raw
}

func r96OneFallbackWire(t *testing.T, before, after string) telegram.Message {
	t.Helper()
	var old, current map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(before), &old))
	require.NoError(t, json.Unmarshal([]byte(after), &current))
	var oldMessages, messages []json.RawMessage
	require.NoError(t, json.Unmarshal(old["Messages"], &oldMessages))
	require.NoError(t, json.Unmarshal(current["Messages"], &messages))
	require.Len(t, oldMessages, 3, "the three original pass notifications already completed")
	require.Len(t, messages, len(oldMessages)+1)
	for index := range oldMessages {
		require.JSONEq(t, string(oldMessages[index]), string(messages[index]))
	}
	var oldNext, next int64
	require.NoError(t, json.Unmarshal(old["Next"], &oldNext))
	require.NoError(t, json.Unmarshal(current["Next"], &next))
	require.Equal(t, oldNext+1, next)
	var added telegram.Message
	require.NoError(t, json.Unmarshal(messages[len(oldMessages)], &added))
	require.Equal(t, next, added.ID)
	require.EqualValues(t, 202, added.Chat.ID)
	require.True(t, added.From.IsBot)
	// Every other fake-state field, including edits/updates/menu, stays equal.
	current["Messages"], current["Next"] = old["Messages"], old["Next"]
	normalized, err := json.Marshal(current)
	require.NoError(t, err)
	require.JSONEq(t, before, string(normalized))
	return added
}

func r96ExactGenericFallback(t *testing.T, f *fixture, language string, message telegram.Message) {
	t.Helper()
	current, err := f.b.API.Current(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(t, "empty", current.State)
	require.Zero(t, current.Version)
	text, err := i18n.Translate(language, i18n.AgentUnavailable, nil)
	require.NoError(t, err)
	state, err := i18n.Translate(language, i18n.WorkflowStateIdle, nil)
	require.NoError(t, err)
	status, err := i18n.Translate(
		language,
		i18n.WorkflowStatus,
		map[string]string{"workflowstate": state, "revision": "0"},
	)
	require.NoError(t, err)
	require.Equal(t, text+"\n\n"+status+"\n", message.Text)
	slots, err := f.b.API.Catalog(t.Context(), "bob")
	require.NoError(t, err)
	rows := make([][]telegram.Button, 0, len(slots))
	for _, slot := range slots {
		label, translateErr := i18n.Translate(language, i18n.WorkflowSlot, map[string]string{
			"title": slot.Title, "price": strconv.Itoa(slot.Price), "currency": slot.Currency,
			"remaining": strconv.Itoa(slot.Remaining),
		})
		require.NoError(t, translateErr)
		rows = append(rows, []telegram.Button{{Text: label, Data: fmt.Sprintf("select:%s:0", slot.ID)}})
	}
	require.Equal(t, telegram.Markup{Rows: rows}, message.Markup)
}

func r96CompletedFallback(
	t *testing.T,
	f *fixture,
	cancelled r96ReceiptCardState,
	message telegram.Message,
) r96ReceiptCardState {
	t.Helper()
	var operations []string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT array_agg(operation_key ORDER BY operation_key)
FROM bot.delivery_intents WHERE bot_id=$1 AND owner='bob' AND message_id=$2 AND effect_key='view'`,
		f.b.Delivery.BotID, message.ID).Scan(&operations))
	require.Len(t, operations, 1)
	state := r96ReceiptCard(t, f, operations[0])
	require.NotEqual(t, cancelled.Intent.Operation, state.Intent.Operation)
	require.Equal(t, delivery.Succeeded, state.Intent.State)
	require.Equal(t, "sent", state.QueueState)
	require.Equal(t, cancelled.QueueChat, state.QueueChat)
	require.Equal(t, cancelled.Thread, state.Thread)
	require.Greater(t, state.LaneSequence, cancelled.LaneSequence, "later live work must pass the cancelled lane head")
	require.Equal(t, "workflow", state.Intent.Reference.Family)
	require.Equal(t, "workflow", state.Intent.Reference.CardKey)
	require.Empty(t, state.Intent.Reference.Object)
	require.Equal(t, message.ID, state.Intent.MessageID)
	require.EqualValues(t, 1, state.Intent.Attempt)
	require.True(t, state.Intent.ContinuationDone)
	require.Equal(t, "workflow_card", state.Intent.Receipt.Kind)
	var projected int64
	var hash string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT message_id,view_hash FROM bot.messages WHERE owner='bob'`).
			Scan(&projected, &hash),
	)
	require.Equal(t, message.ID, projected)
	require.NotEmpty(t, hash)
	require.Equal(t, state.Intent.Receipt.ViewHash, hash)
	return state
}

func r96DrainInvitation(t *testing.T, f *fixture) {
	t.Helper()
	r96InvitationLanes(t, f, "before")
	drainPassNotices(t, f)
	r96InvitationLanes(t, f, "after")
}

func r96InvitationLanes(t *testing.T, f *fixture, phase string) {
	t.Helper()
	var lanes string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(q)
 ORDER BY chat,lane_sequence),'[]'::jsonb)::text FROM core.delivery_queue q WHERE bot_id=$1`, f.b.Delivery.BotID).Scan(&lanes))
	t.Logf("R96_DELIVERY_LANES_%s %s", phase, lanes)
}
