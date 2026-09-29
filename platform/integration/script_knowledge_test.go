package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func runKnowledgeScript(t *testing.T, f *fixture, actor, update int64, code string) json.RawMessage {
	t.Helper()
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, actor, "Use the explicitly requested knowledge operation"))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.Len(t, runs, 1)
	require.Empty(t, runs[0].Error, "%+v", runs[0])
	return runs[0].Result
}

func TestScriptKnowledgeOwnerMemoReplayAndPrivacy(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			f.b.Scripts = scopeVM{}
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			result := runKnowledgeScript(t, f, identity.AliceTelegramID, 15001, `
const before=tools.knowledge.memo_read({fact_key:"diet"});
const saved=tools.knowledge.memo_set({fact_key:"diet",text:"vegetarian / вегетарианское"});
const memos=tools.knowledge.memos();
let forged=false;try {tools.knowledge.memo_set({fact_key:"diet",text:"bad",version:99});forged=true;}catch(_){}
return {before:before.version,version:saved.memo.version,text:memos[0].text,forged};`)
			assert.JSONEq(
				t,
				`{"before":0,"version":1,"text":"vegetarian / вегетарианское","forged":false}`,
				string(result),
			)
			handle(t, f.b, message(15001, identity.AliceTelegramID, "Use the explicitly requested knowledge operation"))
			memos, err := f.b.API.Memos(t.Context(), "alice")
			require.NoError(t, err)
			require.Len(t, memos, 1)
			assert.EqualValues(t, 1, memos[0].Version)
			other, err := f.b.API.Memos(t.Context(), "bob")
			require.NoError(t, err)
			assert.Empty(t, other)
			deliverNotificationBotCards(t, f)
			var visible int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.order_cards WHERE owner='alice' AND card_key LIKE 'knowledge:%'`).
					Scan(&visible),
			)
			assert.Positive(t, visible)
			assertKnowledgeDeleteScriptCascade(t, f)
			memos, err = f.b.API.Memos(t.Context(), "alice")
			require.NoError(t, err)
			assert.Empty(t, memos)
		})
	}
}

func TestScriptKnowledgeSuggestionManualReview(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			f.b.Scripts = scopeVM{}
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id IN ('alice','bob')`, language)
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
			)
			require.NoError(t, err)
			runKnowledgeScript(
				t,
				f,
				identity.AliceTelegramID,
				15011,
				`tools.knowledge.suggest({topic:"travel",fact_key:"venue",text:"Venue opens at noon / Открытие в полдень"});tools.knowledge.suggest({topic:"travel",fact_key:"doors",text:"Use the east door"});return {count:tools.knowledge.proposals({}).items.length};`,
			)
			assert.Equal(t, 2, f.b.Model.(*knowledgeModel).assessments)
			var sources int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.memory_proposal_sources WHERE source_owner='alice'`).
					Scan(&sources),
			)
			assert.Equal(t, 2, sources)
			facts, err := f.b.API.Knowledge(t.Context(), "alice", knowledge.Query{})
			require.NoError(t, err)
			assert.Empty(t, facts)
			proposals, err := f.b.API.KnowledgeProposals(t.Context(), "alice", knowledge.ProposalQuery{})
			require.NoError(t, err)
			require.Len(t, proposals, 2)
			for _, proposal := range proposals {
				require.Equal(t, knowledge.AwaitingSubmission, proposal.State)
			}
			submitKnowledgeCardForAlice(t, f, proposals[0], 15021)
			result := runKnowledgeScript(
				t,
				f,
				identity.BobTelegramID,
				15012,
				`const p=tools.knowledge.review_queue({});const card=tools.knowledge.review_card({proposal_id:p.items[0].id});return card;`,
			)
			assert.JSONEq(t, `{"manual_review_required":true}`, string(result))
			facts, err = f.b.API.Knowledge(t.Context(), "bob", knowledge.Query{})
			require.NoError(t, err)
			assert.Empty(t, facts)
			var approve string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT kind FROM bot.interactions WHERE owner='bob' AND kind LIKE 'knowledge:%' AND content->'command'->>'decision'='approve' LIMIT 1`).
					Scan(&approve),
			)
			callback := aliceCallback(15013, 1, approve)
			callback.Callback.From.ID = identity.BobTelegramID
			callback.Callback.Message.Chat.ID = identity.BobTelegramID
			handle(t, f.b, callback)
			handle(t, f.b, callback)
			facts, err = f.b.API.Knowledge(t.Context(), "alice", knowledge.Query{})
			require.NoError(t, err)
			require.Len(t, facts, 1)
			assert.EqualValues(t, 1, facts[0].Version)
		})
	}
}

type knowledgeMutationVM struct{ afterRead func() }

func (knowledgeMutationVM) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	return nil, fmt.Errorf("unexpected evaluation")
}

func (vm knowledgeMutationVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	return scriptworker.Execute(
		ctx,
		scriptprotocol.ExecuteRequest{Code: request.Code, Input: request.Input, Tools: tools},
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			result, err := callback(ctx, call)
			if call.Name == "knowledge.read" && vm.afterRead != nil {
				vm.afterRead()
				vm.afterRead = nil
			}
			return result, err
		},
	)
}

func TestScriptKnowledgeCuratorStaleAndRevoke(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','curate')`,
	)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	result := runKnowledgeScript(
		t,
		f,
		identity.AliceTelegramID,
		15021,
		`tools.knowledge.read({topic:"travel",fact_key:"venue"});const r=tools.knowledge.curate({topic:"travel",fact_key:"venue",text:"first"});return {version:r.fact.version};`,
	)
	assert.JSONEq(t, `{"version":1}`, string(result))
	f.b.Scripts = knowledgeMutationVM{afterRead: func() {
		_, mutateErr := f.b.API.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.Curate,
				Key:     "concurrent",
				Topic:   "travel",
				FactKey: "venue",
				Text:    "concurrent",
				Version: 1,
			},
		)
		require.NoError(t, mutateErr)
	}}
	result = runKnowledgeScript(
		t,
		f,
		identity.AliceTelegramID,
		15022,
		`tools.knowledge.read({topic:"travel",fact_key:"venue"});let changed=false;try{tools.knowledge.curate({topic:"travel",fact_key:"venue",text:"stale"});changed=true;}catch(_){}return {changed};`,
	)
	assert.JSONEq(t, `{"changed":false}`, string(result))
	fact, err := f.b.API.KnowledgeFact(t.Context(), "alice", "", "travel", "venue")
	require.NoError(t, err)
	assert.Equal(t, "concurrent", fact.Text)
	f.b.Scripts = scopeVM{before: func() {
		_, deleteErr := f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
		require.NoError(t, deleteErr)
	}}
	result = runKnowledgeScript(
		t,
		f,
		identity.AliceTelegramID,
		15023,
		`const listed=tools.$list().some(t=>t.name==="knowledge.curate");let help=false,called=false;try{tools.knowledge.curate.$help();help=true;}catch(_){}try{tools.knowledge.curate({topic:"travel",fact_key:"venue",text:"revoked"});called=true;}catch(_){}return {listed,help,called};`,
	)
	assert.JSONEq(t, `{"listed":false,"help":false,"called":false}`, string(result))
}

func TestScriptKnowledgeCompleteFactPages(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','curate')`,
	)
	require.NoError(t, err)
	body := strings.Repeat("界", 1900)
	for i := range 25 {
		_, err = f.b.API.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.Curate,
				Key:     fmt.Sprintf("seed-%d", i),
				Topic:   "travel",
				FactKey: fmt.Sprintf("key-%02d", i),
				Text:    body,
			},
		)
		require.NoError(t, err)
	}
	result := runKnowledgeScript(
		t,
		f,
		identity.AliceTelegramID,
		15031,
		`let cursor="",count=0,length=0;do{const p=tools.knowledge.read({topic:"travel",cursor});count+=p.items.length;length+=p.items.reduce((n,x)=>n+x.text.length,0);cursor=p.next_cursor;if(!p.more)break;}while(cursor);return {count,length};`,
	)
	assert.JSONEq(t, `{"count":25,"length":47500}`, string(result))
	f.b.Scripts = knowledgeMutationVM{afterRead: func() {
		_, changeErr := f.b.API.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.Curate,
				Key:     "page-change",
				Topic:   "travel",
				FactKey: "key-00",
				Text:    "changed",
				Version: 1,
			},
		)
		require.NoError(t, changeErr)
	}}
	result = runKnowledgeScript(
		t,
		f,
		identity.AliceTelegramID,
		15032,
		`const p=tools.knowledge.read({topic:"travel"});return tools.knowledge.read({topic:"travel",cursor:p.next_cursor});`,
	)
	assert.JSONEq(t, `{"error":"stale","restart":true}`, string(result))
}

func TestScriptKnowledgeHistoricalScopesAndProposals(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) SELECT 'old-'||lpad(n::text,2,'0'),clock_timestamp()-n*interval '1 day' FROM generate_series(1,25) n;
 INSERT INTO core.knowledge_scopes(scope,event_id) SELECT id,id FROM core.pass_events WHERE id LIKE 'old-%';
 INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('old-25','alice','curate');
 INSERT INTO core.knowledge_proposals(scope,owner,topic,fact_key,body,state,fact_version) SELECT '', 'alice','travel','proposal-'||n,'Past suggestion '||n,'filtered',0 FROM generate_series(1,23) n`,
	)
	require.NoError(t, err)
	result := runKnowledgeScript(t, f, identity.AliceTelegramID, 15041, `
let cursor="",count=0,found=false;do{const p=tools.knowledge.scopes({cursor});count+=p.items.length;found=found||p.items.some(x=>x.event==="old-25"&&x.can_curate);cursor=p.next_cursor;if(!p.more)break;}while(cursor);
tools.knowledge.read({event:"old-25",topic:"travel",fact_key:"venue"});
const fact=tools.knowledge.curate({event:"old-25",topic:"travel",fact_key:"venue",text:"Historical event correction"});
const p=tools.knowledge.proposals({});const p2=tools.knowledge.proposals({cursor:p.next_cursor});
return {count,found,version:fact.fact.version,proposals:p.items.length+p2.items.length,more:p2.more};`)
	var parsed struct {
		Count     int  `json:"count"`
		Found     bool `json:"found"`
		Version   int  `json:"version"`
		Proposals int  `json:"proposals"`
		More      bool `json:"more"`
	}
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.GreaterOrEqual(t, parsed.Count, 26)
	assert.True(t, parsed.Found)
	assert.Equal(t, 1, parsed.Version)
	assert.Equal(t, 23, parsed.Proposals)
	assert.False(t, parsed.More)
}

// Deleting memory now also revokes the prior derived archive and its inherited
// script generation. The ordinary script helper still requires no error.
func assertKnowledgeDeleteScriptCascade(t *testing.T, f *fixture) {
	t.Helper()
	history := conversation.Service{DB: f.db}
	before, err := history.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	code := `tools.knowledge.memo_read({fact_key:"diet"});tools.knowledge.memo_delete({fact_key:"diet"});return {remaining:tools.knowledge.memos().length};`
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	f.b.Model = model
	update := message(15002, identity.AliceTelegramID, "Use the explicitly requested knowledge operation")
	handle(t, f.b, update)
	require.Len(t, model.inputs, 2)
	require.Len(t, model.inputs[1].Script.Runs, 1)
	run := model.inputs[1].Script.Runs[0]
	require.True(t, run.PassRedacted)
	require.Equal(t, "history_deleted", run.Error)
	require.Empty(t, run.Calls)
	require.Empty(t, run.Code)
	require.Empty(t, run.Result)
	after, err := history.Window(t.Context(), "alice", 10)
	require.NoError(t, err)
	require.Greater(t, after.Generation, before.Generation)
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "vegetarian")
	require.NotContains(t, string(encoded), "вегетарианское")
	// Replay after a fresh bot instance cannot re-run the committed deletion.
	f.b = &bot.Bot{
		DB:      f.db,
		API:     f.b.API,
		Host:    f.b.Host,
		TG:      f.b.TG,
		Scripts: scopeVM{},
		Model: avModel(func(context.Context, agent.Input) (agent.Plan, error) {
			t.Error("replay invoked model")
			return agent.Plan{}, context.Canceled
		}),
	}
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal history plan")
	var deletes int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='alice' AND result->'memo'->>'key'='diet' AND result->'memo'->>'version'='2' AND result->'memo'->>'active'='false'`).
			Scan(&deletes),
	)
	require.Equal(t, 1, deletes)
	page, err := history.Read(t.Context(), "alice", conversation.Query{Limit: 20})
	require.NoError(t, err)
	encoded, err = json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "vegetarian")
	require.NotContains(t, string(encoded), "вегетарианское")
}
