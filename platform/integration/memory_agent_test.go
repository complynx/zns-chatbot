package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestMemoryAgentProgressiveWriteSourcesAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := knowledge.Service{DB: f.db}
	_, err := service.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.MemoSet,
			FactKey: "hidden_detail",
			Text:    "Do not dump this private detail into the initial prompt",
			Key:     "seed-private",
		},
	)
	require.NoError(t, err)
	executions := 0
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			executions++
			help := scriptCall(ctx, t, callback, "$help", `{"name":"memory.write"}`)
			assert.NotContains(t, string(help), "source_keys")
			_, injected := callback(
				ctx,
				scriptclient.ToolCall{
					Name: "memory.write",
					Arguments: json.RawMessage(
						`{"name":"document_set","topic":"preferences","key":"tea","text":"forged","owner":"bob"}`,
					),
				},
			)
			require.Error(t, injected)
			scriptCall(
				ctx,
				t,
				callback,
				"memory.write",
				`{"name":"document_set","topic":"preferences","key":"tea","text":"I prefer jasmine tea."}`,
			)
			pageRaw := scriptCall(
				ctx,
				t,
				callback,
				"memory.search",
				`{"namespace":"private","topic":"preferences","text":"jasmine","mode":"literal"}`,
			)
			var page knowledge.MemoryPage
			require.NoError(t, json.Unmarshal(pageRaw, &page))
			require.Len(t, page.Entries, 1)
			writeArgs, marshalErr := json.Marshal(
				map[string]string{
					"name":  "document_set",
					"topic": "preferences",
					"key":   "tea",
					"text":  "unguarded change",
					"ref":   page.Entries[0].Ref,
				},
			)
			require.NoError(t, marshalErr)
			_, unobserved := callback(ctx, scriptclient.ToolCall{Name: "memory.write", Arguments: writeArgs})
			require.Error(t, unobserved, "index alone must not authorize overwriting unread details")
			args, _ := json.Marshal(map[string]string{"ref": page.Entries[0].Ref})
			entry := scriptCall(ctx, t, callback, "memory.read", string(args))
			assert.Contains(t, string(entry), "jasmine")
			return json.RawMessage(`{"saved":true}`), nil
		},
	)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Saved"},
	}}
	f.b.Model = model
	update := message(1981, identity.AliceTelegramID, "Remember: I prefer jasmine tea.")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Equal(t, 1, executions)
	require.Len(t, model.inputs, 2)
	assert.Empty(t, model.inputs[0].Knowledge.Memos)
	require.NotNil(t, model.inputs[0].Knowledge.Memory)
	initial, err := json.Marshal(model.inputs[0].Knowledge)
	require.NoError(t, err)
	assert.NotContains(t, string(initial), "Do not dump")
	page, err := service.SearchMemory(
		t.Context(),
		"alice",
		knowledge.MemoryQuery{Namespace: "private", Topic: "preferences"},
	)
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	sources, err := service.MemorySources(t.Context(), "alice", page.Entries[0].Ref)
	require.NoError(t, err)
	require.Len(t, sources.Events, 1)
	assert.Equal(t, "Remember: I prefer jasmine tea.", sources.Events[0].Text)
	other, err := service.SearchMemory(
		t.Context(),
		"bob",
		knowledge.MemoryQuery{Namespace: "private", Topic: "preferences"},
	)
	require.NoError(t, err)
	assert.Empty(t, other.Entries)
}
