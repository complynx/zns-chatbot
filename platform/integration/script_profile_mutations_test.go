package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestScriptProfileSemanticWritesLanguagesPrivacyAndReplay(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			handleVisible(t, f.b, message(9800, 101, "/profile"))
			f.b.Scripts = scopeVM{}
			code := `const name=tools.profile.set({field:"legal_name",value:"Private Canary Name"});
const passport=tools.profile.set({field:"passport",value:"Private Passport Canary"});
const role=tools.profile.set({field:"role",value:"follower"});
const language=tools.preferences.setLanguage({language:"` + language + `"});
const current=tools.profile.get();
if(current.passport!=="Private Passport Canary" || current.role!=="follower") throw new Error("state mismatch");
return {name,passport,role,language,privateEcho:current.passport};`
			model := &knowledgeModel{plans: []agent.Plan{
				{View: "profile", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
				{View: "profile", Text: "Saved"},
			}}
			f.b.Model = model
			text := "Save my name Private Canary Name, passport Private Passport Canary, role follower and language " + language
			if language == "ru" {
				text = "Сохрани имя Private Canary Name, паспорт Private Passport Canary, роль фолловер и русский язык"
			}
			update := message(9801, 101, text)
			handle(t, f.b, update)
			handle(t, f.b, update)
			require.Len(t, model.inputs, 2)
			assert.Equal(t, language, model.inputs[1].Language)
			run := model.inputs[1].Script.Runs[0]
			require.Empty(t, run.Error)
			require.Len(t, run.Calls, 5)
			assert.JSONEq(t, `{"field":"legal_name","applied":true,"version":1}`, string(run.Calls[0].Result))
			assert.NotContains(t, string(run.Result), "Private")
			profile, err := f.b.API.PassProfile(t.Context(), "alice")
			require.NoError(t, err)
			assert.Equal(t, "Private Canary Name", profile.LegalName)
			assert.Equal(t, "Private Passport Canary", profile.Passport)
			assert.Equal(t, "follower", profile.Role)
			assert.EqualValues(t, 3, profile.Version)
			other, err := f.b.API.PassProfile(t.Context(), "bob")
			require.NoError(t, err)
			assert.Empty(t, other.Passport)
			var receipts string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=9801 AND kind='script_runs'`).
					Scan(&receipts),
			)
			assert.NotContains(t, receipts, "Private Canary")
			assert.NotContains(t, receipts, "Private Passport")
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
					Scan(&count),
			)
			assert.Equal(t, 3, count)
			// A later manual action sees the semantic write and the selected locale.
			handleVisible(t, f.b, message(9802, 101, "/profile"))
			history, err := f.b.API.PassProfileHistoryPage(t.Context(), "alice", 0)
			require.NoError(t, err)
			require.Len(t, history.Items, 3)
			assert.Equal(t, "role", history.Items[0].Field)
			card := profileCard(t, f)
			assert.Contains(t, card.Text, "Private Canary Name")
			assert.NotContains(t, card.Text, "Private Passport Canary")
			title, err := i18n.Translate(language, i18n.ProfileTitle, nil)
			require.NoError(t, err)
			assert.Contains(t, card.Text, title)
			var archive string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT COALESCE(string_agg(text,' '),'') FROM core.conversation_events WHERE owner='alice'`).
					Scan(&archive),
			)
			assert.NotContains(t, archive, "Private Canary Name")
			assert.NotContains(t, archive, "Private Passport Canary")
			f.b.Model = f.model
			f.model.plan = agent.Plan{View: "workflow", Text: "Other question answered"}
			handle(t, f.b, message(9803, 101, "What is the schedule?"))
			assert.Equal(t, language, f.model.input.Language)
			unchanged, err := f.b.API.PassProfile(t.Context(), "alice")
			require.NoError(t, err)
			assert.Equal(t, profile, unchanged)
		})
	}
}

func TestScriptProfileCompleteHistoryAndHiddenWrite(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := passes.Service{DB: f.db}
	for i := range int64(47) {
		_, err := service.Execute(
			t.Context(),
			"alice",
			passes.Command{
				Name:    "set",
				Field:   "role",
				Value:   "leader",
				Version: i,
				Key:     "history-" + strconv.FormatInt(i, 10),
				Origin:  "manual",
			},
		)
		require.NoError(t, err)
	}
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: "workflow",
			ScriptAction: &agent.ScriptProposal{
				Code:      `let before=0,versions=[]; do { const page=tools.profile.history({before}); versions=versions.concat(page.items.map(x=>x.version)); before=page.next_before||0; } while(before); return versions;`,
				InputJSON: "null",
			},
		},
		{View: "workflow", Text: "Read"},
	}}
	f.b.Model = model
	handle(t, f.b, message(9810, 101, "Read all my profile changes"))
	var versions []int64
	require.NoError(t, json.Unmarshal(model.inputs[1].Script.Runs[0].Result, &versions))
	require.Len(t, versions, 47)
	assert.EqualValues(t, 47, versions[0])
	assert.EqualValues(t, 1, versions[46])
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	model = &knowledgeModel{plans: []agent.Plan{
		{
			View: "workflow",
			ScriptAction: &agent.ScriptProposal{
				Code:      `const list=tools.$list(); let hidden=false; try { tools.profile.set.$help(); } catch(e) { hidden=true; } return {hidden,listed:list.some(x=>x.name==="profile.set")};`,
				InputJSON: "null",
			},
		},
		{View: "workflow", Text: "Read"},
	}}
	f.b.Model = model
	handle(t, f.b, message(9811, 101, "Read available tools"))
	assert.JSONEq(t, `{"hidden":true,"listed":false}`, string(model.inputs[1].Script.Runs[0].Result))
}

type profileInterleavingTransport struct {
	before func()
	done   bool
}

func (transport *profileInterleavingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost &&
		(request.URL.Path == "/v1/me/pass-profile/actions" || request.URL.Path == "/internal/derived/pass-profiles") &&
		!transport.done {
		transport.done = true
		transport.before()
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestScriptProfileStaleAndRevokedAtExecution(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"stale", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			transport := &profileInterleavingTransport{before: func() {
				if mode == "revoked" {
					_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
					require.NoError(t, err)
					return
				}
				_, err := (passes.Service{DB: f.db}).Execute(
					t.Context(),
					"alice",
					passes.Command{
						Name:    "set",
						Field:   "role",
						Value:   "leader",
						Version: 0,
						Key:     "manual-race",
						Origin:  "manual",
					},
				)
				require.NoError(t, err)
			}}
			f.b.API.HTTP = &http.Client{Transport: transport}
			f.b.Host.HTTP = f.b.API.HTTP
			f.b.Scripts = scopeVM{}
			model := &knowledgeModel{plans: []agent.Plan{
				{
					View: "profile",
					ScriptAction: &agent.ScriptProposal{
						Code:      `tools.profile.set({field:"role",value:"follower"}); return tools.profile.get();`,
						InputJSON: "null",
					},
				},
				{View: "profile", Text: "Current state inspected"},
			}}
			f.b.Model = model
			handle(t, f.b, message(9820, 101, "Set my dance role to follower"))
			require.True(t, transport.done)
			run := model.inputs[1].Script.Runs[0]
			require.NotEmpty(t, run.Calls)
			if mode == "stale" {
				assert.Equal(t, "stale", run.Calls[0].Error)
				require.Len(t, run.Calls, 2)
			} else {
				assert.Equal(t, "denied", run.Calls[0].Error)
			}
			current, err := f.b.API.PassProfile(t.Context(), "alice")
			require.NoError(t, err)
			assert.NotEqual(t, "follower", current.Role)
		})
	}
}
