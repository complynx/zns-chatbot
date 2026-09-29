package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type interruptedMemoVM struct {
	scopeVM

	cancel context.CancelFunc
}

func (vm *interruptedMemoVM) Execute(ctx context.Context, request scriptclient.Request,
	tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	vm.cancel = cancel
	_, _ = vm.scopeVM.Execute(child, request, tools, callback)
	return nil, context.DeadlineExceeded
}

type memoReceiptTransport struct {
	after func()
	calls atomic.Int64
}

func (transport *memoReceiptTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil || request.URL.Path != "/internal/knowledge/derived" {
		return response, err
	}
	transport.calls.Add(1)
	if response.StatusCode != http.StatusOK {
		return response, nil
	}
	_ = response.Body.Close()
	if transport.after != nil {
		transport.after()
	}
	return nil, io.ErrUnexpectedEOF
}

func scriptMemoRecords(t *testing.T, f *fixture, update int64) []agenthost.ScriptRecord {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		"SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'",
		update,
	).Scan(&records))
	return records
}

func TestScriptMemoTimeoutRecoversCommittedCallOnly(t *testing.T) {
	t.Parallel()
	f := setup(t)
	vm := &interruptedMemoVM{}
	transport := &memoReceiptTransport{after: func() { vm.cancel() }}
	f.b.Scripts = vm
	f.b.Host.HTTP = &http.Client{Transport: transport}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{
			Code:      `await tools.knowledge.memo_set({fact_key:"first",text:"private-receipt-canary"}); return tools.knowledge.memo_set({fact_key:"unexecuted",text:"must-not-run"});`,
			InputJSON: "null",
		}},
		{View: agent.KnowledgeView, Text: "The first operation committed; the script was interrupted."},
	}}
	f.b.Model = model
	update := message(79501, 101, "Save my first memo and then the second memo")
	handle(t, f.b, update)
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.Len(t, runs, 1)
	assert.Equal(t, "timeout", runs[0].Error)
	require.Len(t, runs[0].Calls, 1)
	assert.Empty(t, runs[0].Calls[0].Error)
	assert.JSONEq(t, `{"committed":true,"version":1}`, string(runs[0].Calls[0].Result))
	assert.NotContains(t, string(runs[0].Calls[0].Result), "private-receipt-canary")
	memos, err := f.b.API.Memos(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, memos, 1)
	assert.Equal(t, "first", memos[0].Key)
	records := scriptMemoRecords(t, f, update.ID)
	require.Len(t, records[0].Calls, 1)
	assert.False(t, records[0].Calls[0].KnowledgeRefreshPending)
	assert.NotNil(t, records[0].Calls[0].Source)
	handle(t, f.b, update)
	assert.EqualValues(t, 1, transport.calls.Load())
	assert.Len(t, model.inputs, 2)
}

func TestScriptMemoReceiptCannotRestoreDeletedHistory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	vm := &interruptedMemoVM{}
	transport := &memoReceiptTransport{after: func() {
		vm.cancel()
		var id int64
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), "SELECT min(id) FROM core.conversation_events WHERE owner='alice'").Scan(&id),
		)
		require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", id))
	}}
	f.b.Scripts = vm
	f.b.Host.HTTP = &http.Client{Transport: transport}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: agent.KnowledgeView,
			ScriptAction: &agent.ScriptProposal{
				Code:      `return tools.knowledge.memo_set({fact_key:"private",text:"deleted-history-canary"});`,
				InputJSON: "null",
			},
		},
		{View: agent.KnowledgeView, Text: "No private result restored"},
	}}
	f.b.Model = model
	_ = f.b.Handle(t.Context(), message(79502, 101, "Save this private memo"))
	records := scriptMemoRecords(t, f, 79502)
	require.Len(t, records, 1)
	require.Len(t, records[0].Calls, 1)
	assert.NotEmpty(t, records[0].Calls[0].Outcome.Error)
	assert.Empty(t, records[0].Calls[0].Outcome.Result)
	for _, input := range model.inputs[1:] {
		encoded, err := json.Marshal(input.Script)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "deleted-history-canary")
	}
	assert.EqualValues(t, 1, transport.calls.Load())
}

type failedKnowledgeRefreshTransport struct {
	fixture     *fixture
	failed      atomic.Bool
	unavailable atomic.Bool
	delivered   atomic.Int64
	reference   atomic.Pointer[delivery.Reference]
}

func (transport *failedKnowledgeRefreshTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	card := false
	if strings.Contains(request.URL.Path, "sendMessage") || strings.Contains(request.URL.Path, "editMessage") {
		err := transport.fixture.db.QueryRow(request.Context(), `SELECT EXISTS(
 SELECT 1 FROM bot.delivery_intents WHERE owner='alice' AND state='sending'
 AND reference->>'family'='knowledge')`).Scan(&card)
		if err != nil {
			return nil, err
		}
		if card && transport.unavailable.Load() {
			ref := &delivery.Reference{Owner: delivery.Bot}
			if err = transport.fixture.db.QueryRow(request.Context(), `SELECT operation_key,effect_key
 FROM bot.delivery_intents WHERE owner='alice' AND state='sending'
 AND reference->>'family'='knowledge'`).Scan(&ref.Key, &ref.Effect); err != nil {
				return nil, err
			}
			transport.reference.Store(ref)
			transport.failed.Store(true)
			// Reject before forwarding: this explicit provider response is definitely unsent.
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(
					strings.NewReader(
						`{"ok":false,"error_code":429,"description":"synthetic refresh unavailable","parameters":{"retry_after":0}}`,
					),
				),
				Request: request,
			}, nil
		}
		if ref := transport.reference.Load(); card && ref != nil {
			err = transport.fixture.db.QueryRow(request.Context(), `SELECT EXISTS(
 SELECT 1 FROM bot.delivery_intents WHERE operation_key=$1 AND effect_key=$2 AND state='sending')`,
				ref.Key, ref.Effect).Scan(&card)
			if err != nil {
				return nil, err
			}
		}
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if card && err == nil && response.StatusCode == http.StatusOK {
		transport.delivered.Add(1)
	}
	return response, err
}

func TestScriptMemoRefreshFailureKeepsDurableOutcome(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	transport := &failedKnowledgeRefreshTransport{fixture: f}
	transport.unavailable.Store(true)
	f.b.TG.HTTP = &http.Client{Transport: transport}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{
			Code:      `return tools.knowledge.memo_set({fact_key:"durable",text:"saved"});`,
			InputJSON: "null",
		}},
		{View: agent.KnowledgeView, Text: "Saved"},
	}}
	f.b.Model = model
	update := message(79503, 101, "Save my durable memo")
	handle(t, f.b, update)
	require.False(t, transport.failed.Load(), "Handle hands presentation to durable delivery")
	records := scriptMemoRecords(t, f, update.ID)
	require.Len(t, records, 1)
	require.Len(t, records[0].Calls, 1)
	assert.Empty(t, records[0].Calls[0].Outcome.Error)
	assert.False(t, records[0].Calls[0].KnowledgeRefreshPending, "durable enqueue completes the host handoff")

	// Select authoritative shared lane heads and stop immediately after the rejected attempt.
	for range 32 {
		tx, err := f.db.Begin(t.Context())
		require.NoError(t, err)
		entries, err := delivery.Candidates(t.Context(), tx, f.b.Delivery.BotID, 100)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(t.Context()))
		for _, entry := range entries {
			if entry.Reference.Owner == delivery.Bot {
				require.NoError(t, f.b.DeliverBotIntent(t.Context(), entry.Reference))
			}
			if transport.failed.Load() {
				break
			}
		}
		if transport.failed.Load() {
			break
		}
	}
	require.True(t, transport.failed.Load())
	require.Zero(t, transport.delivered.Load())
	var reference delivery.Reference
	reference.Owner = delivery.Bot
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'family'='knowledge' AND state='pending' AND attempt=1`).Scan(&reference.Key, &reference.Effect))
	handle(t, f.b, update)
	records = scriptMemoRecords(t, f, update.ID)
	assert.Empty(t, records[0].Calls[0].Outcome.Error)
	assert.False(t, records[0].Calls[0].KnowledgeRefreshPending)
	transport.unavailable.Store(false)
	deliverNotificationBotCards(t, f)
	var state string
	var attempts, messageID int64
	var continued bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state,attempt,message_id,continuation_done
 FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		f.b.Delivery.BotID, reference.Key, reference.Effect).Scan(&state, &attempts, &messageID, &continued))
	assert.Equal(t, string(delivery.Succeeded), state)
	assert.EqualValues(t, 2, attempts)
	assert.Positive(t, messageID)
	assert.True(t, continued)
	assert.EqualValues(t, 1, transport.delivered.Load())
	// Replaying both the input and the exact terminal delivery cannot repeat either effect.
	handle(t, f.b, update)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), reference))
	assert.EqualValues(t, 1, transport.delivered.Load())
	var effects, receipts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_audit
 WHERE actor='alice' AND action='memo_set'`).Scan(&effects))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations
 WHERE actor='alice' AND result->'memo'->>'key'='durable' AND result->'memo'->>'version'='1'`).Scan(&receipts))
	assert.Equal(t, 1, effects)
	assert.Equal(t, 1, receipts)
	require.Len(t, model.inputs, 2)
}
