package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestCodeQADeletedMemoNotRetainedInModelInput(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	_, err := f.b.API.ExecuteKnowledge(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "qa-seed", FactKey: "diet", Text: "PRIVATE-DELETED-CANARY"},
	)
	require.NoError(t, err)
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: agent.KnowledgeView,
			ScriptAction: &agent.ScriptProposal{
				Code:      `tools.knowledge.memos(); tools.knowledge.memo_delete({fact_key:"diet"}); return {done:true};`,
				InputJSON: "null",
			},
		},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	f.b.Model = model
	handle(t, f.b, message(15991, identity.AliceTelegramID, "Delete my diet preference"))
	require.Len(t, model.inputs, 2)
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	assert.NotContains(
		t,
		string(encoded),
		"PRIVATE-DELETED-CANARY",
		"deleted memo must not survive in the next model input",
	)
}

type memoChangeVM struct {
	afterRead  func()
	executions *int
}

func (memoChangeVM) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	return nil, errors.New("unexpected evaluation")
}

func (vm memoChangeVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	if vm.executions != nil {
		*vm.executions++
	}
	return scriptworker.Execute(
		ctx,
		scriptprotocol.ExecuteRequest{Code: request.Code, Input: request.Input, Tools: tools},
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			result, err := callback(ctx, call)
			if call.Name == "knowledge.memos" && vm.afterRead != nil {
				vm.afterRead()
				vm.afterRead = nil
			}
			return result, err
		},
	)
}

func TestScriptKnowledgeMemoListRetainsUsableObservation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	_, err := f.b.API.ExecuteKnowledge(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "seed", FactKey: "diet", Text: "CURRENT-PRIVATE-MEMO"},
	)
	require.NoError(t, err)
	result := runKnowledgeScript(
		t,
		f,
		identity.AliceTelegramID,
		15992,
		`const memos=tools.knowledge.memos();return {text:memos[0].text};`,
	)
	assert.JSONEq(t, `{"text":"CURRENT-PRIVATE-MEMO"}`, string(result))
	encoded, err := json.Marshal(f.b.Model.(*knowledgeModel).inputs[1].Knowledge)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "CURRENT-PRIVATE-MEMO")
	assert.Empty(
		t,
		f.b.Model.(*knowledgeModel).inputs[1].Knowledge.Memos,
		"memo observations use deletion-stamped reads",
	)
}

func TestScriptKnowledgeMemoListDoesNotRebindConcurrentVersion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.ExecuteKnowledge(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "seed", FactKey: "diet", Text: "initial"},
	)
	require.NoError(t, err)
	f.b.Scripts = memoChangeVM{afterRead: func() {
		_, changeErr := f.b.API.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.MemoSet,
				Key:     "manual-change",
				FactKey: "diet",
				Text:    "manual change",
				Version: 1,
			},
		)
		require.NoError(t, changeErr)
	}}
	result := runKnowledgeScript(t, f, identity.AliceTelegramID, 15993, `
 tools.knowledge.memos();let staleBlocked=false;
 try{tools.knowledge.memo_set({fact_key:"diet",text:"unobserved overwrite"});}catch(_){staleBlocked=true;}
 const fresh=tools.knowledge.memo_read({fact_key:"diet"});
 const saved=tools.knowledge.memo_set({fact_key:"diet",text:"explicit after fresh read"});
 return {staleBlocked,fresh:fresh.text,version:saved.memo.version};`)
	assert.JSONEq(t, `{"staleBlocked":true,"fresh":"manual change","version":3}`, string(result))
}

func TestScriptKnowledgeExternalMemoDeletionClearsNextPlan(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.ExecuteKnowledge(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "seed", FactKey: "diet", Text: "EXTERNAL-DELETION-CANARY"},
	)
	require.NoError(t, err)
	executions := 0
	f.b.Scripts = memoChangeVM{executions: &executions, afterRead: func() {
		_, changeErr := f.b.API.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{Name: knowledge.MemoDelete, Key: "manual-delete", FactKey: "diet", Version: 1},
		)
		require.NoError(t, changeErr)
	}}
	code := `
 const old=tools.knowledge.memos();let staleBlocked=false;
 try{tools.knowledge.memo_delete({fact_key:"diet"});}catch(_){staleBlocked=true;}
 const fresh=tools.knowledge.memo_read({fact_key:"diet"});
 return {old,staleBlocked,active:fresh.active};`
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	f.b.Model = model
	handle(t, f.b, message(15994, identity.AliceTelegramID, "Use the explicitly requested knowledge operation"))
	require.Len(t, model.inputs, 2)
	require.Len(t, model.inputs[1].Script.Runs, 1)
	run := model.inputs[1].Script.Runs[0]
	assert.Equal(t, "interrupted", run.Error)
	assert.JSONEq(t, `{"omitted":true,"reason":"memory_deleted"}`, string(run.Result))
	assert.Equal(t, 1, executions)
	var records []struct {
		MemoryRedacted bool              `json:"memory_redacted"`
		Calls          []json.RawMessage `json:"calls"`
		Run            agent.ScriptRun   `json:"run"`
	}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=15994 AND kind='script_runs'`).Scan(&records))
	require.Len(t, records, 1)
	assert.True(t, records[0].MemoryRedacted)
	assert.Equal(t, "interrupted", records[0].Run.Error)
	assert.Len(t, records[0].Calls, 1, "stale mutation must not be admitted")
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "EXTERNAL-DELETION-CANARY")
	memo, err := f.b.API.Memo(t.Context(), "alice", "diet")
	require.NoError(t, err)
	assert.False(t, memo.Active)
	assert.EqualValues(t, 2, memo.Version)
}

func TestScriptKnowledgeMemoDeleteThenListPreservesRemainingMemo(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	for key, text := range map[string]string{"diet": "REMOVED-PRIVATE-CANARY", "drink": "RETAINED-PRIVATE-MEMO"} {
		_, err := f.b.API.ExecuteKnowledge(
			t.Context(),
			"alice",
			knowledge.Command{Name: knowledge.MemoSet, Key: "seed-" + key, FactKey: key, Text: text},
		)
		require.NoError(t, err)
	}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{
			Code:      `tools.knowledge.memos();tools.knowledge.memo_delete({fact_key:"diet"});return tools.knowledge.memos();`,
			InputJSON: "null",
		}},
		{View: agent.KnowledgeView, Text: "The deletion committed; the script was interrupted."},
	}}
	f.b.Model = model
	update := message(15995, identity.AliceTelegramID, "Delete my diet memo and list remaining memos")
	handle(t, f.b, update)
	require.Len(t, model.inputs, 2)
	require.Len(t, model.inputs[1].Script.Runs, 1)
	assert.Equal(t, "interrupted", model.inputs[1].Script.Runs[0].Error)
	assert.JSONEq(t, `{"omitted":true,"reason":"memory_deleted"}`, string(model.inputs[1].Script.Runs[0].Result))
	records := scriptMemoRecords(t, f, update.ID)
	require.Len(t, records, 1)
	assert.True(t, records[0].MemoryRedacted)
	assert.Equal(t, "interrupted", records[0].Run.Error)
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "REMOVED-PRIVATE-CANARY")
	assert.Contains(t, string(encoded), "RETAINED-PRIVATE-MEMO")
	memos, err := f.b.API.Memos(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, memos, 1)
	assert.Equal(t, "drink", memos[0].Key)
	assert.EqualValues(t, 1, memos[0].Version)
	handle(t, f.b, update)
	assert.Len(t, model.inputs, 2, "replay must not reopen the retired script")
	deleted, err := f.b.API.Memo(t.Context(), "alice", "diet")
	require.NoError(t, err)
	assert.False(t, deleted.Active)
	assert.EqualValues(t, 2, deleted.Version)
	var effects, receipts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_audit
 WHERE actor='alice' AND action='memo_delete' AND subject='diet'`).Scan(&effects))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations
 WHERE actor='alice' AND result->'memo'->>'key'='diet'
 AND result->'memo'->>'version'='2' AND result->'memo'->>'active'='false'`).Scan(&receipts))
	assert.Equal(t, 1, effects)
	assert.Equal(t, 1, receipts, "one canonical deletion receipt survives script retirement")
}

// retirementPolicy injects a one-shot completion failure after durable admission.
// Retired reloads must still check the current access decision.
type retirementPolicy struct {
	finished bool
	memory   bool
	history  bool
	revoked  bool
	failure  error
}

func (p *retirementPolicy) Generation(context.Context, string) (int64, error) {
	if p.finished && p.history {
		return 1, nil
	}
	return 0, nil
}
func (p *retirementPolicy) MemoryState(context.Context, string) (knowledge.MemoryDeletionState, error) {
	state := knowledge.MemoryDeletionState{}
	if p.finished && p.memory {
		state.PrivateGeneration = 1
	}
	return state, nil
}
func (p *retirementPolicy) AccessChanged(context.Context, string, agenthost.ScriptRecord) (bool, error) {
	if !p.finished {
		return false, nil
	}
	failure := p.failure
	p.failure = nil
	return p.revoked || failure != nil, failure
}

func (*retirementPolicy) KnowledgeReceipt(
	context.Context,
	string,
	knowledge.Command,
	readsource.Derivation,
) (derivedmutation.Receipt[knowledge.Result], error) {
	return derivedmutation.Receipt[knowledge.Result]{
		Found:  true,
		Result: knowledge.Result{PrivateDeletion: &knowledge.PrivateDeletionWitness{Current: true}},
	}, nil
}

type retirementVM struct{ finish func() }

func (vm retirementVM) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	vm.finish()
	return json.RawMessage(`{"private":"retired-canary"}`), nil
}
func TestScriptRetirementCompletionDoesNotHideFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name                                      string
		receipt, memory, history, revoked, joined bool
	}{
		{name: "owned_joined_failure", receipt: true, memory: true, joined: true},
		{name: "owned_permission_retirement", receipt: true, memory: true, revoked: true},
		{name: "owned_history_retirement", receipt: true, history: true},
		{name: "external_permission_and_memory", memory: true, revoked: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			stale := errors.New("retirement stale")
			failure := errors.New("transient authorization failure")
			policy := &retirementPolicy{memory: scenario.memory, history: scenario.history, revoked: scenario.revoked}
			if scenario.joined {
				policy.failure = failure
			}
			executions := 0
			host := agenthost.ScriptHost{
				Store: agenthost.ScriptStore{
					DB:         db,
					Reads:      agenthost.ReadStore{DB: db, Memory: agenthost.MemoryReadStore{DB: db}},
					Policy:     policy,
					StaleError: stale,
				},
			}
			host.Worker = retirementVM{finish: func() {
				executions++
				if scenario.receipt {
					call := agenthost.ScriptToolRecord{
						Memory:  &knowledge.Command{Name: knowledge.MemoDelete, Key: "delete", FactKey: "private"},
						Source:  &readsource.Derivation{},
						Outcome: agent.ScriptToolResult{Name: "knowledge.memo_delete"},
					}
					raw, err := json.Marshal([]agenthost.ScriptToolRecord{call})
					require.NoError(t, err)
					_, err = db.Exec(
						t.Context(),
						`UPDATE bot.interactions SET content=jsonb_set(content,'{0,calls}',$1::jsonb) WHERE owner='alice' AND update_id=15996 AND kind='script_runs'`,
						raw,
					)
					require.NoError(t, err)
				}
				policy.finished = true
			}}
			input := agent.Input{Script: &agent.ScriptContext{Remaining: 1}}
			err := host.Perform(
				t.Context(),
				"alice",
				15996,
				agent.ScriptProposal{Code: "return 1;", InputJSON: "null"},
				&input,
			)
			require.ErrorIs(t, err, stale)
			if scenario.joined {
				require.ErrorIs(t, err, failure)
			}
			assert.Equal(t, 1, executions)
		})
	}
}

func TestScriptMemoryRetirementRechecksPaymentScope(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPrivilegedReads(t, f)
	_, err := f.b.API.ExecuteKnowledge(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "seed", FactKey: "private", Text: "mixed-retirement-canary"},
	)
	require.NoError(t, err)
	executions := 0
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			executions++
			scriptCall(ctx, t, callback, "passes.payments.history", `{"event":"script-dance"}`)
			scriptCall(ctx, t, callback, "knowledge.memos", `{}`)
			_, deleteErr := f.b.API.ExecuteKnowledge(
				t.Context(),
				"alice",
				knowledge.Command{Name: knowledge.MemoDelete, Key: "external-delete", FactKey: "private", Version: 1},
			)
			require.NoError(t, deleteErr)
			_, deleteErr = f.db.Exec(
				t.Context(),
				`DELETE FROM core.pass_payment_admins WHERE event_id='script-dance' AND owner='alice'`,
			)
			require.NoError(t, deleteErr)
			return json.RawMessage(`{"private":"mixed-retirement-canary"}`), nil
		},
	)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: "return {};", InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "must not continue after permission retirement"},
	}}
	f.b.Model = model
	require.NoError(
		t,
		f.b.Handle(t.Context(), message(15997, identity.AliceTelegramID, "Read my payment history and private memo")),
	)
	require.Len(t, model.inputs, 1, "permission retirement must not permit another model call")
	assert.Equal(t, 1, executions)
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=15997 AND kind='script_runs'`).
			Scan(&records),
	)
	require.Len(t, records, 1)
	require.Len(t, records[0].Calls, 2)
	require.NotNil(t, records[0].Calls[0].PrivilegedRead, "revoked payment scope must survive result redaction")
	assert.Equal(t, "script-dance", records[0].Calls[0].PrivilegedRead.Event)
}

func TestScriptOwnedDeletionRejectsUnrelatedRetirement(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"history", "payment_scope"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			seedPrivilegedReads(t, f)
			archive := conversation.Service{DB: f.db}
			require.NoError(
				t,
				archive.AppendOriginal(t.Context(), "alice", "unrelated-private", "user", "unrelated-history-canary"),
			)
			var eventID int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='unrelated-private'`).
					Scan(&eventID),
			)
			_, err := f.b.API.ExecuteKnowledge(
				t.Context(),
				"alice",
				knowledge.Command{
					Name:    knowledge.MemoSet,
					Key:     "seed",
					FactKey: "private",
					Text:    "owned-deletion-canary",
				},
			)
			require.NoError(t, err)
			executions := 0
			f.b.Scripts = hostScriptFunc(
				func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
					executions++
					scriptCall(ctx, t, callback, "passes.payments.history", `{"event":"script-dance"}`)
					scriptCall(ctx, t, callback, "knowledge.memo_read", `{"fact_key":"private"}`)
					_, _ = callback(
						ctx,
						scriptclient.ToolCall{
							Name:      "knowledge.memo_delete",
							Arguments: json.RawMessage(`{"fact_key":"private"}`),
						},
					)
					memo, readErr := f.b.API.Memo(t.Context(), "alice", "private")
					require.NoError(t, readErr)
					require.False(t, memo.Active, "the owned deletion must commit before the unrelated retirement")
					if mode == "history" {
						require.NoError(t, archive.DeleteContent(t.Context(), "alice", eventID))
					} else {
						_, deleteErr := f.db.Exec(
							t.Context(),
							`DELETE FROM core.pass_payment_admins WHERE event_id='script-dance' AND owner='alice'`,
						)
						require.NoError(t, deleteErr)
					}
					return json.RawMessage(`{"done":true}`), nil
				},
			)
			model := &knowledgeModel{plans: []agent.Plan{
				{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: "return {};", InputJSON: "null"}},
				{View: agent.KnowledgeView, Text: "must not continue after unrelated retirement"},
			}}
			f.b.Model = model
			update := message(15998, identity.AliceTelegramID, "Delete my private memo after reading payment history")
			handleErr := f.b.Handle(t.Context(), update)
			if mode == "history" {
				require.ErrorIs(t, handleErr, appclient.ErrReadStale)
			} else {
				require.NoError(t, handleErr)
			}
			require.Len(t, model.inputs, 1, "an owned receipt cannot explain an independent retirement")
			assert.Equal(t, 1, executions)
			memo, err := f.b.API.Memo(t.Context(), "alice", "private")
			require.NoError(t, err)
			assert.False(t, memo.Active)
			assert.EqualValues(t, 2, memo.Version)
			replayErr := f.b.Handle(t.Context(), update)
			if mode == "history" {
				require.ErrorIs(t, replayErr, appclient.ErrReadStale)
			} else {
				require.NoError(t, replayErr)
			}
			assert.Len(t, model.inputs, 1)
			assert.Equal(t, 1, executions, "replay must not rerun the retired VM or deletion")
		})
	}
}
