package observability_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestKnowledgeToolOperationDiagnosticsRemainFinite(t *testing.T) {
	t.Parallel()
	operations := []string{
		"knowledge.read",
		"knowledge.scopes",
		"knowledge.proposals",
		"knowledge.review_queue",
		"knowledge.memos",
		"knowledge.memo_read",
		"knowledge.memo_set",
		"knowledge.memo_delete",
		"knowledge.suggest",
		"knowledge.curate",
		"knowledge.remove_fact",
		"knowledge.review_card",
	}
	var log bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	for _, operation := range operations {
		recorder.Emit(t.Context(), observability.AgentEvent{Phase: "tool", Operation: operation, Outcome: "ok"})
	}
	for _, operation := range []string{"knowledge.PRIVATE-CANARY", "knowledge.memo_read.PRIVATE-CANARY", "PRIVATE-CANARY"} {
		recorder.Emit(
			t.Context(),
			observability.AgentEvent{
				Phase:     "tool",
				Operation: operation,
				Outcome:   "error",
				ErrorCode: "PRIVATE-CANARY",
			},
		)
	}
	require.NotContains(t, log.String(), "PRIVATE-CANARY")
	var exported bytes.Buffer
	_, err = observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&exported,
		observability.AgentExportOptions{Limit: 100},
	)
	require.NoError(t, err)
	decoder := json.NewDecoder(&exported)
	for _, operation := range append(operations, "unknown", "unknown", "unknown") {
		var record observability.AgentRecord
		require.NoError(t, decoder.Decode(&record))
		assert.Equal(t, operation, record.Operation)
		if operation == "unknown" {
			assert.Equal(t, "unknown", record.ErrorCode)
		}
	}
}
