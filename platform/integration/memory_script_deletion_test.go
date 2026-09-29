package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestMemoryScriptDeletionRedactsReplayWithoutReexecution(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := knowledge.Service{DB: f.db}
	_, err := service.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.DocumentSet,
			Topic:   "notes",
			FactKey: "secret",
			Text:    "deleted-memory-marker",
			Key:     "delete-seed",
		},
	)
	require.NoError(t, err)
	ledger := json.RawMessage(
		`[{"request":{"code":"return 'deleted-memory-marker';","input_json":"{\"text\":\"deleted-memory-marker\"}"},"run":{"code":"return 'deleted-memory-marker';","result":{"text":"deleted-memory-marker"}},"calls":[{"memory":{"name":"document_set","topic":"notes","fact_key":"secret","text":"deleted-memory-marker","key":"delete-seed"},"outcome":{"name":"memory.read","result":{"text":"deleted-memory-marker"},"error":""}}]}]`,
	)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',1985,'script_runs',$1)`,
		ledger,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES('alice',1985,'knowledge_reads','[{"memo":{"key":"secret","text":"deleted-memory-marker","version":1,"active":true}}]')`)
	require.NoError(t, err)
	_, err = service.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.DocumentDelete,
			Topic:   "notes",
			FactKey: "secret",
			Version: 1,
			Key:     "delete-memory",
		},
	)
	require.NoError(t, err)
	model := &knowledgeModel{plans: []agent.Plan{{View: agent.KnowledgeView, Text: "No retained script data."}}}
	f.b.Model = model
	handle(t, f.b, message(1985, 101, "Continue the interrupted request"))
	require.Len(t, model.inputs, 1)
	projected, err := json.Marshal(model.inputs[0].Script)
	require.NoError(t, err)
	assert.NotContains(t, string(projected), "deleted-memory-marker")
	assert.Contains(t, string(projected), "memory_deleted")
	legacy, err := json.Marshal(model.inputs[0].Knowledge.Reads)
	require.NoError(t, err)
	assert.NotContains(t, string(legacy), "deleted-memory-marker")
	assert.Contains(t, string(legacy), "memory_deleted")
	var retained string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=1985 AND kind='script_runs'`).
			Scan(&retained),
	)
	assert.NotContains(t, retained, "deleted-memory-marker")
	assert.Contains(t, retained, "delete-seed", "effect replay identity must survive redaction")
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=1985 AND kind='knowledge_reads'`).
			Scan(&retained),
	)
	assert.NotContains(t, retained, "deleted-memory-marker")
	state, err := service.DocumentState(t.Context(), "alice", "notes", "secret")
	require.NoError(t, err)
	assert.False(t, state.Active)
	assert.EqualValues(t, 2, state.Version, "script replay must not reapply the write")
}
