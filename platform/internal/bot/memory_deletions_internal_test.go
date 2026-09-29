package bot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestDeletedScriptCannotRestorePayloadOnLateFinish(t *testing.T) {
	t.Parallel()
	var state knowledge.MemoryDeletionState
	require.NoError(t, json.Unmarshal([]byte(`{"private_generation":1,"shared_generation":0}`), &state))
	record := scriptRecord{Run: agent.ScriptRun{Result: json.RawMessage(`"secret"`)}}
	assert.True(t, redactDeletedScript(&record, state))
	record.Run.Result = json.RawMessage(`"late-secret"`)
	assert.True(t, redactDeletedScript(&record, state))
	assert.NotContains(t, string(record.Run.Result), "secret")
	assert.Contains(t, string(record.Run.Result), "memory_deleted")
}
