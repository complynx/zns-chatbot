package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestScriptPassArchivedAssignedWithoutPayment(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`DELETE FROM core.legacy_pass_payment_metadata WHERE event_id='archive'; UPDATE core.pass_bookings SET state='assigned',price=100 WHERE event_id='archive'`,
	)
	require.NoError(t, err)
	home := runPassVM(
		t,
		f,
		29800,
		101,
		"Show archive home",
		`return tools.passes.registration.show({event:"archive",view:"home"});`,
	)
	require.Empty(t, home.Error)
	pumpBotDeliveries(t, f.b)
	t.Logf("home card: %s", passMenuCard(t, f, 101).Text)
	handleVisible(t, f.b, passMenuClick(t, f, 101, 29801, "Pass payment"))
	require.Contains(t, passMenuCard(t, f, 101).Text, "No payment has been recorded")
	t.Logf("payment callback card: %s", passMenuCard(t, f, 101).Text)
	var state struct {
		Event      string `json:"event"`
		Historical bool   `json:"historical"`
		View       string `json:"view"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT state FROM bot.pass_views WHERE owner='alice'`).
			Scan(&state),
	)
	t.Logf("payment state: %+v", state)
	read := runPassVM(
		t,
		f,
		29802,
		101,
		"Read archive payment",
		`return tools.passes.registration.read({event:"archive",view:"payment"});`,
	)
	t.Logf("read: error=%q result=%s", read.Error, read.Result)
	var result agent.RegistrationReadResult
	require.NoError(t, json.Unmarshal(read.Result, &result))
	require.Empty(
		t,
		result.Error,
		"owned historical payment view should stay readable without an uploaded receipt",
	)
	require.True(t, result.Historical)
	require.Nil(t, result.Payment)
	service := passbooking.Service{DB: f.db}
	_, err = service.Payment(t.Context(), "alice", "archive", "alice")
	problem, ok := errors.AsType[*core.ProblemError](err)
	require.True(t, ok)
	require.Equal(t, "pass_payment_missing", problem.Code)
	_, err = service.Payment(t.Context(), "bob", "archive", "alice")
	problem, ok = errors.AsType[*core.ProblemError](err)
	require.True(t, ok)
	require.Equal(t, "forbidden", problem.Code)
	_, err = service.PaymentProof(t.Context(), "alice", "archive", "alice")
	require.Error(t, err)
	require.Equal(t, "archive", state.Event)
	require.True(t, state.Historical)
	require.Equal(t, "payment", state.View)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	shown := runPassVM(
		t,
		f,
		29803,
		101,
		"Покажи оплату архивного пасса",
		`return tools.passes.registration.show({event:"archive",view:"payment"});`,
	)
	require.Empty(t, shown.Error)
	pumpBotDeliveries(t, f.b)
	require.Contains(t, passMenuCard(t, f, 101).Text, "Оплата этого пасса не записана")
}

func TestScriptPassCurrentAssignedPaymentUpload(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	// The domain registration queues a deferred pass notice at Alice's lane head;
	// drive domain and Bot delivery so the manual card is actually delivered.
	drainPassNotices(t, f)
	handlePassVisible(t, f, message(29830, 101, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 29831, "Dance"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 29832, "Pass payment"))
	card := passMenuCard(t, f, 101).Text
	require.Contains(t, card, "Total to pay:")
	require.Contains(t, card, "Send a receipt photo or document")
	require.NotContains(t, card, "No payment has been recorded")
	service := passbooking.Service{DB: f.db}
	assigned, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, "assigned", assigned.State)
	require.Equal(t, "bob", assigned.PaymentAdmin)
	photo, body := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "100", Currency: "RUB",
	}}
	handle(t, f.b, photo)
	booked, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, assigned.Version+1, booked.Version)
	require.Equal(t, "paid", booked.State)
	require.Equal(t, assigned.AssignedAt, booked.AssignedAt)
	require.Equal(t, "bob", booked.PaymentAdmin)
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, "pending", payment.Decision)
	require.Equal(t, booked.Version, payment.Version)
	require.Equal(t, "bob", payment.ReceivingAdmin)
	proof, err := f.b.API.DownloadPassProof(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, body, proof.Body)
	require.Equal(t, booked.Version, proof.Version)
	require.Equal(t, payment.Attempt, proof.Attempt)
	queue, err := service.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1, "the assigned payment admin sees the receipt")
	require.Equal(t, "alice", queue.Items[0].Owner)
	require.Equal(t, booked.CreatedAt, queue.Items[0].BookingCreatedAt)
	require.Equal(t, booked.Version, queue.Items[0].Payment.Version)
	require.Equal(t, "bob", queue.Items[0].Payment.ReceivingAdmin)
	require.Equal(t, payment.Attempt, queue.Items[0].Payment.Attempt)
	handle(t, f.b, photo)
	replayed, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, booked, replayed, "replaying the same upload must preserve booking version and assignment")
	replayedPayment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, payment, replayedPayment)
	replayedProof, err := f.b.API.DownloadPassProof(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, proof, replayedProof, "replay preserves original bytes and versioned proof binding")
	var attempts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&attempts))
	require.Equal(t, 1, attempts, "upload and replay record exactly one payment attempt")
	_, err = service.PaymentQueue(t.Context(), "alice", "dance", "")
	requireCode(t, err, "forbidden")
}

func TestScriptPassArchivedRemovedReadRetry(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment='archive-private-canary' WHERE event_id='archive'`,
	)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `tools.preferences.setLanguage({language:"ru"}); return tools.passes.registration.read({event:"archive",view:"home"});`,
					InputJSON: "null",
				},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[0].Result), "archive-private-canary")
		if calls == 2 {
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `return {derived:input};`,
					InputJSON: `"archive-private-canary"`,
				},
			}, nil
		}
		require.Contains(t, string(input.Script.Runs[1].Result), "archive-private-canary")
		return agent.Plan{}, context.Canceled
	})
	update := message(29810, 101, "Read my archive registration")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.legacy_pass_payment_metadata WHERE event_id='archive'; DELETE FROM core.pass_bookings WHERE event_id='archive' AND owner='alice'`,
	)
	require.NoError(t, err)
	var input agent.Input
	f.b.Model = avModel(func(_ context.Context, current agent.Input) (agent.Plan, error) {
		input = current
		return agent.Plan{}, context.Canceled
	})
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	require.Equal(t, "forbidden", input.Registration.Reads[0].Error)
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	require.NotContains(
		t,
		string(raw), "archive-private-canary",
		"removed historical read survives in Script.Runs[0].Result: %s",
		input.Script.Runs[0].Result,
	)
	require.Zero(t, input.Script.Remaining)
	require.Len(t, input.Script.Runs, 2)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=29810 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, "archive-private-canary")
	require.Contains(t, stored, `"pass_redacted": true`)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,comment)
 VALUES('archive','alice',1,'assigned','leader','guest','bob','2025-09-01','2025-09-02',100,'archive-private-canary')`,
	)
	require.NoError(t, err)
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	handle(t, f.b, update)
	require.Len(t, model.inputs, 1)
	raw, err = json.Marshal(model.inputs[0].Script)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "archive-private-canary")
	var operations int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).
			Scan(&operations),
	)
	require.Equal(t, 1, operations)
	require.Contains(t, stored, `"language": {`)
}

type archivedPassLateVM struct {
	scopeVM

	after func()
}

func (vm archivedPassLateVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	result, err := vm.scopeVM.Execute(ctx, request, tools, callback)
	vm.after()
	return result, err
}

func TestScriptPassArchivedLateResultReauthorized(t *testing.T) {
	t.Parallel()
	for name, code := range map[string]string{
		"registration": `const r=await tools.passes.registration.read({event:"archive",view:"home"}); return {derived:r.booking.comment};`,
		"get":          `const r=await tools.passes.get({event:"archive"}); return {derived:r.comment};`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertArchivedLateResult(t, code)
		})
	}
}

func assertArchivedLateResult(t *testing.T, code string) {
	t.Helper()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment='late-private-canary' WHERE event_id='archive'`,
	)
	require.NoError(t, err)
	workerRuns := 0
	f.b.Scripts = hostScriptFunc(func(
		ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback,
	) (json.RawMessage, error) {
		workerRuns++
		request := scriptclient.Request{Code: code, Input: json.RawMessage(`null`)}
		result, runErr := (scopeVM{}).Execute(ctx, request, tools, callback)
		require.NoError(t, runErr)
		require.Contains(t, string(result), "late-private-canary", "prove the original private source reached the VM")
		_, updateErr := f.db.Exec(
			t.Context(),
			`UPDATE core.pass_bookings SET version=version+1,comment='' WHERE event_id='archive'`,
		)
		require.NoError(t, updateErr)
		return result, runErr
	})
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: "workflow",
			ScriptAction: &agent.ScriptProposal{
				Code:      code,
				InputJSON: "null",
			},
		},
		{View: "workflow", Text: "Checked"},
	}}
	f.b.Model = model
	update := message(29820, 101, "Read my archive registration")
	handleVisible(t, f.b, update)
	require.Len(t, model.inputs, 1, "a changed source ends the admitted turn")
	retired := archivedLateRetirement(t, f)
	handleVisible(t, f.b, update)
	require.Len(t, model.inputs, 1, "same-update replay must not replan")
	handleVisible(t, f.b, message(29821, 101, "Read my current available information"))
	require.Len(t, model.inputs, 2)
	require.Equal(t, 1, workerRuns)
	require.NotNil(t, model.inputs[1].Script)
	require.EqualValues(t, 29821, model.inputs[1].Script.UpdateID)
	require.Empty(t, model.inputs[1].Script.Runs)
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "late-private-canary")
	require.JSONEq(t, retired, archivedLateRetirement(t, f), "new input must not resurrect the old execution")
}

func archivedLateRetirement(t *testing.T, f *fixture) string {
	t.Helper()
	var raw string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions
 WHERE owner='alice' AND update_id=29820 AND kind='script_runs'`).Scan(&raw))
	require.NotContains(t, raw, "late-private-canary")
	require.Contains(t, raw, "pass_access_changed")
	var retired []struct {
		PassRedacted bool                 `json:"pass_redacted"`
		Request      agent.ScriptProposal `json:"request"`
		Run          agent.ScriptRun      `json:"run"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &retired))
	require.Len(t, retired, 1)
	require.True(t, retired[0].PassRedacted)
	require.Empty(t, retired[0].Request.Code)
	require.Empty(t, retired[0].Request.InputJSON)
	require.Empty(t, retired[0].Run.Code)
	require.Empty(t, retired[0].Run.Calls)
	require.JSONEq(t, `{"omitted":true,"reason":"pass_access_changed"}`, string(retired[0].Run.Result))
	var kind, state string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT kind,state FROM interaction.saved_turns
 WHERE owner='alice' AND update_id=29820`).Scan(&kind, &state))
	require.Equal(t, "notice", kind)
	require.Equal(t, "ready", state)
	messages := chatMessages(t, f, 101)
	require.NotEmpty(t, messages)
	for _, msg := range messages {
		require.NotContains(t, msg.Text, "late-private-canary")
	}
	return raw
}
