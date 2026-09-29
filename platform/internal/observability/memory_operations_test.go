package observability_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestMemoryOperationNamesSurviveDiagnosticExport(t *testing.T) {
	t.Parallel()
	names := []string{
		"memory.summary",
		"memory.index",
		"memory.search",
		"memory.read",
		"memory.history",
		"memory.revision",
		"memory.sources",
		"memory.write",
	}
	var log, exported bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	for _, name := range names {
		recorder.Emit(t.Context(), observability.AgentEvent{Phase: "tool", Operation: name, Outcome: "ok"})
	}
	recorder.Emit(
		t.Context(),
		observability.AgentEvent{Phase: "tool", Operation: "memory.private-secret", Outcome: "ok"},
	)
	_, err = observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&exported,
		observability.AgentExportOptions{Limit: 100},
	)
	require.NoError(t, err)
	require.NotContains(t, exported.String(), "private-secret")
	decoder := json.NewDecoder(&exported)
	for _, name := range append(names, "unknown") {
		var record observability.AgentRecord
		require.NoError(t, decoder.Decode(&record))
		require.Equal(t, name, record.Operation)
	}
}
