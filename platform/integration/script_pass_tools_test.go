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
)

func runPassVM(t *testing.T, f *fixture, update, user int64, text, code string) agent.ScriptRun {
	t.Helper()
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{
		plans: []agent.Plan{
			{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
			{View: "workflow", Text: "Checked"},
		},
	}
	f.b.Model = model
	handle(t, f.b, message(update, user, text))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.NotEmpty(t, runs)
	return runs[len(runs)-1]
}

func TestScriptPassOwnerReadsMutationExactResume(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	result := runPassVM(t, f, 19000, 101, "Invite Telegram ID 202 to dance", `
 await tools.passes.registration.read({event:"dance",view:"home"});
 return tools.passes.registration.invite({event:"dance",invite_telegram_id:202});`)
	require.Empty(t, result.Error)
	var first struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &first))
	require.True(t, first.Complete)
	require.NotEmpty(t, first.ID)
	service := passbooking.Service{DB: f.db}
	current, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", bookingCommand("cancel", "later-user-cancel", current))
	require.NoError(t, err)
	replay := runPassVM(
		t,
		f,
		19001,
		101,
		"Resume my pass action",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, first.ID),
	)
	require.Empty(t, replay.Error)
	var resumed struct {
		ID     string              `json:"operation_id"`
		Result passbooking.Booking `json:"result"`
	}
	require.NoError(t, json.Unmarshal(replay.Result, &resumed))
	assert.Equal(t, first.ID, resumed.ID)
	assert.Equal(t, "cancelled", resumed.Result.State, "the existing API returns current state on exact command replay")
	var operations int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).
			Scan(&operations),
	)
	assert.Equal(t, 2, operations)
	current, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", current.State)
	foreign := runPassVM(
		t,
		f,
		19002,
		202,
		"Resume",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, first.ID),
	)
	assert.NotEmpty(t, foreign.Error)
	listed := runPassVM(t, f, 19003, 101, "Show my operations", `return tools.passes.operations({});`)
	require.Empty(t, listed.Error)
	assert.Contains(t, string(listed.Result), first.ID)
	assert.NotContains(t, string(listed.Result), "tg-script")
}

func TestScriptPassDiscoveryRestrictedAdminAndRevocation(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.users SET can_book=false WHERE id IN ('alice','bob'); DELETE FROM core.pass_booking_admins WHERE owner='bob'`,
	)
	require.NoError(t, err)
	code := `return (await tools.$list()).map(t=>t.name).filter(n=>n.startsWith("passes."));`
	own := runPassVM(t, f, 19100, 101, "List tools", code)
	require.Empty(t, own.Error)
	assert.NotContains(t, string(own.Result), "passes.registration.solo")
	assert.NotContains(t, string(own.Result), "passes.admin")
	assert.Contains(t, string(own.Result), "passes.registration.read")
	admin := runPassVM(t, f, 19101, 202, "List tools", code)
	require.Empty(t, admin.Error)
	assert.Contains(t, string(admin.Result), "passes.batch.cancel")
	assert.Contains(t, string(admin.Result), "passes.payments.accept")
	assert.NotContains(t, string(admin.Result), "passes.batch.assign")
	assert.NotContains(t, string(admin.Result), "passes.export")
	f.b.Scripts = scopeVM{before: func() {
		_, deleteErr := f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
		require.NoError(t, deleteErr)
	}}
	model := &knowledgeModel{
		plans: []agent.Plan{
			{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `let help=false,called=false; try{await tools.passes.batch.cancel.$help();help=true;}catch(_){} try{await tools.passes.batch.cancel({event:"dance",recipients:[101]});called=true;}catch(_){} return {help,called};`,
					InputJSON: "null",
				},
			},
			{View: "workflow", Text: "Checked"},
		},
	}
	f.b.Model = model
	handle(t, f.b, message(19102, 202, "Cancel pass for 101"))
	require.Len(t, model.inputs, 2)
	run := model.inputs[1].Script.Runs[0]
	require.Empty(t, run.Error)
	assert.JSONEq(t, `{"help":false,"called":false}`, string(run.Result))
}

type passLostReply struct{ lost bool }

func (transport *passLostReply) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil &&
		(request.URL.Path == "/v1/passes/batches" || request.URL.Path == "/internal/derived/pass-batches") &&
		!transport.lost {
		transport.lost = true
		_ = response.Body.Close()
		return nil, errors.New("synthetic lost batch reply")
	}
	return response, err
}

func TestScriptPassBatchLostReplyResumeAndRevoke(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Test Name'`)
	require.NoError(t, err)
	transport := &passLostReply{}
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	result := runPassVM(
		t,
		f,
		19200,
		202,
		"Assign 101 to dance from their profile",
		`return tools.passes.batch.assign({event:"dance",recipients:[101],assignment:{create:true,from_profile:true}});`,
	)
	require.Empty(t, result.Error)
	var pending struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &pending))
	assert.False(t, pending.Complete)
	require.NotEmpty(t, pending.ID)
	service := passbooking.Service{DB: f.db}
	before, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", before.State)
	retry := runPassVM(
		t,
		f,
		19201,
		202,
		"Resume batch",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, pending.ID),
	)
	require.Empty(t, retry.Error)
	var completed map[string]any
	require.NoError(t, json.Unmarshal(retry.Result, &completed))
	assert.Equal(t, true, completed["complete"])
	again := runPassVM(
		t,
		f,
		19202,
		202,
		"Resume batch again",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, pending.ID),
	)
	require.Empty(t, again.Error)
	assert.JSONEq(t, string(retry.Result), string(again.Result))
	after, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, before, after)
	var batches int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_batches`).Scan(&batches))
	assert.Equal(t, 1, batches)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	denied := runPassVM(
		t,
		f,
		19203,
		202,
		"Resume batch",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, pending.ID),
	)
	assert.NotEmpty(t, denied.Error)
}

func TestScriptPassTierAndExportReceipt(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	result := runPassVM(
		t,
		f,
		19300,
		202,
		"Export passes and show tier status",
		`const tiers=await tools.passes.tiers({event:"dance"}); const a=await tools.passes.export({}); const b=await tools.passes.export({});return {tiers,a,b};`,
	)
	require.Empty(t, result.Error)
	var exported struct {
		A struct {
			ID       string `json:"operation_id"`
			Complete bool   `json:"complete"`
			Result   struct {
				Delivered bool `json:"delivered"`
			} `json:"result"`
		} `json:"a"`
		B struct {
			ID       string `json:"operation_id"`
			Complete bool   `json:"complete"`
		} `json:"b"`
		Tiers struct {
			More bool `json:"more"`
		} `json:"tiers"`
	}
	require.NoError(t, json.Unmarshal(result.Result, &exported))
	require.NotEmpty(t, exported.A.ID)
	require.Equal(t, exported.A.ID, exported.B.ID)
	require.False(t, exported.A.Complete)
	require.False(t, exported.B.Complete)
	require.False(t, exported.A.Result.Delivered)
	assert.False(t, exported.Tiers.More)
	var receipts, intents int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND kind='pass_export'`).
			Scan(&receipts),
	)
	require.Zero(t, receipts)
	pumpBotDeliveries(t, f.b)
	resumed := runPassVM(
		t,
		f,
		19301,
		202,
		"Check export",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, exported.A.ID),
	)
	require.Empty(t, resumed.Error)
	require.NoError(t, json.Unmarshal(resumed.Result, &exported.A))
	require.True(t, exported.A.Complete)
	require.True(t, exported.A.Result.Delivered)
	require.Equal(t, exported.B.ID, exported.A.ID)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND kind='pass_export'`).
			Scan(&receipts),
	)
	require.Equal(t, 1, receipts)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents WHERE owner='bob' AND reference->>'family'='pass_export'`).
			Scan(&intents),
	)
	require.Equal(t, 1, intents)
	assert.NotContains(t, string(result.Result), "UEsDB")
	assert.Less(t, len(result.Result), 5000)
}
