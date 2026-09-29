package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type profileLostReplyTransport struct{ lost bool }

func (transport *profileLostReplyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil && request.Method == http.MethodPost &&
		(request.URL.Path == "/v1/me/pass-profile/actions" || request.URL.Path == "/internal/derived/pass-profiles") &&
		!transport.lost {
		transport.lost = true
		_ = response.Body.Close()
		return nil, errors.New("synthetic lost response")
	}
	return response, err
}

func TestScriptProfileLostWriteReadRecoveryAndTypedFollowup(t *testing.T) {
	t.Parallel()
	f := setup(t)
	transport := &profileLostReplyTransport{}
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: "profile",
			ScriptAction: &agent.ScriptProposal{
				Code:      `try { tools.profile.set({field:"legal_name",value:"Private Recovery Name"}); } catch(e) {} return tools.profile.get();`,
				InputJSON: "null",
			},
		},
		{
			View:          "profile",
			Text:          "Saved requested role after reading current state",
			ProfileAction: &agent.ProfileProposal{Name: "set", Field: "role", Value: "follower"},
		},
	}}
	f.b.Model = model
	update := message(9830, 101, "Save my legal name Private Recovery Name and dance role follower")
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.True(t, transport.lost, "lost-response fault must fire on the profile mutation")
	require.Len(t, model.inputs, 2)
	assert.EqualValues(t, 1, model.inputs[1].Profile.Version)
	calls := model.inputs[1].Script.Runs[0].Calls
	require.Len(t, calls, 2)
	assert.Equal(t, "interrupted", calls[0].Error)
	current, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "Private Recovery Name", current.LegalName)
	assert.Equal(t, "follower", current.Role)
	assert.EqualValues(t, 2, current.Version)
}

func TestScriptProfileRejectedPrivateValueIsNotProvenance(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: "profile",
			ScriptAction: &agent.ScriptProposal{
				Code:      `try { tools.profile.set({field:"passport",value:"Private Rejected Canary",owner:"bob"}); } catch(e) {} return "Private Rejected Canary";`,
				InputJSON: "null",
			},
		},
		{View: "profile", Text: "Invalid request"},
	}}
	f.b.Model = model
	handle(t, f.b, message(9831, 101, "Please save Private Rejected Canary"))
	require.Len(t, model.inputs, 2)
	assert.NotContains(t, string(model.inputs[1].Script.Runs[0].Result), "Private Rejected Canary")
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=9831 AND kind='script_runs'`).
			Scan(&stored),
	)
	assert.NotContains(t, stored, "Private Rejected Canary")
	var archived string
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT COALESCE(string_agg(text,' '),'') FROM core.conversation_events WHERE owner='alice'`).Scan(&archived))
	assert.NotContains(t, archived, "Private Rejected Canary")
	assert.Contains(t, archived, "[private profile submission omitted]")
	current, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Empty(t, current.Passport)
}

type profileReceiptVM struct{ check func(scriptclient.ToolCall) }

func (profileReceiptVM) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	return nil, errors.New("legacy evaluator unused")
}

func (vm profileReceiptVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	return (scopeVM{}).Execute(
		ctx,
		request,
		tools,
		func(callCtx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			result, err := callback(callCtx, call)
			vm.check(call)
			return result, err
		},
	)
}

func TestScriptProfileReceiptsRedactedBeforeWorkerTerminates(t *testing.T) {
	t.Parallel()
	f := setup(t)
	checked := 0
	f.b.Scripts = profileReceiptVM{check: func(call scriptclient.ToolCall) {
		checked++
		var stored string
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=9832 AND kind='script_runs'`).
				Scan(&stored),
		)
		assert.NotContains(t, stored, "Private Interrupted Canary", call.Name)
		assert.Contains(t, stored, `"error": "interrupted"`)
	}}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: "profile",
			ScriptAction: &agent.ScriptProposal{
				Code:      `tools.profile.set({field:"passport",value:"Private Interrupted Canary"}); const p=tools.profile.get(); if(p.passport!=="Private Interrupted Canary") throw new Error("lost live value"); tools.preferences.setLanguage({language:"ru"}); throw new Error("synthetic worker termination");`,
				InputJSON: "null",
			},
		},
		{View: "profile", Text: "Effects require current-state inspection"},
	}}
	f.b.Model = model
	handle(t, f.b, message(9832, 101, "Save passport Private Interrupted Canary and use Russian"))
	assert.Equal(t, 3, checked)
	require.Len(t, model.inputs, 2)
	assert.NotEmpty(t, model.inputs[1].Script.Runs[0].Error)
	preference, err := f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "ru", preference.Language)
	current, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "Private Interrupted Canary", current.Passport)
}
