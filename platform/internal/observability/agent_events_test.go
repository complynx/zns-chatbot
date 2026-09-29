package observability_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestAgentEventsOrderAndPrivateValues(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		2,
	)
	require.NoError(t, err)
	parent := recorder.Emit(
		t.Context(),
		observability.AgentEvent{Phase: "request", Operation: "telegram.update", Outcome: "started"},
	)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			recorder.Emit(
				t.Context(),
				observability.AgentEvent{
					Phase:     "tool",
					Operation: "memo-secret-canary",
					Outcome:   "invalid",
					ErrorCode: "private-canary",
					Parent:    parent,
				},
			)
		})
	}
	workers.Wait()
	recorder.EmitCode(t.Context(), observability.AgentEvent{Phase: "script", Operation: "js.run", Outcome: "ok"},
		`const privateCanary = "memo-secret-canary"; return input.filter(x => x.secret === privateCanary).map(x => x);`)
	require.NotContains(t, log.String(), "canary")
	require.NotContains(t, log.String(), "privateCanary")
	var out bytes.Buffer
	page, err := observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&out,
		observability.AgentExportOptions{Limit: 100},
	)
	require.NoError(t, err)
	require.Equal(t, 10, page.Records)
	decoder := json.NewDecoder(&out)
	for sequence := uint64(1); sequence <= 10; sequence++ {
		var record observability.AgentRecord
		require.NoError(t, decoder.Decode(&record))
		require.Equal(t, sequence, record.Sequence)
		require.Equal(t, uint32(2), record.Attempt)
	}
}

func TestAgentLogContinuationAndPartialTail(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	recorder.Emit(t.Context(), observability.AgentEvent{Phase: "tool", Operation: "read", Outcome: "invalid"})
	recorder.Emit(t.Context(), observability.AgentEvent{Phase: "tool", Operation: "read", Outcome: "ok", Replay: true})
	var first, second bytes.Buffer
	page, err := observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&first,
		observability.AgentExportOptions{Limit: 1},
	)
	require.NoError(t, err)
	require.True(t, page.Incomplete)
	next, err := observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&second,
		observability.AgentExportOptions{Limit: 10, Offset: page.NextOffset},
	)
	require.NoError(t, err)
	require.Equal(t, 1, next.Records)
	require.False(t, next.Incomplete)
	require.Contains(t, second.String(), `"replay":true`)
	_, err = observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&second,
		observability.AgentExportOptions{Limit: 10, Offset: 3},
	)
	require.Error(t, err)
	log.WriteString(`{"agent_event":"unfinished-private-canary`)
	tail, err := observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&second,
		observability.AgentExportOptions{Limit: 10, Offset: next.NextOffset},
	)
	require.NoError(t, err)
	require.Equal(t, "partial_line", tail.Reason)
	require.Equal(t, next.NextOffset, tail.NextOffset)
	require.NotContains(t, second.String(), "canary")
}

func TestAgentCodeProfileNeverReturnsSource(t *testing.T) {
	t.Parallel()
	cases := []string{
		`/* canary-comment */ const canaryIdentifier = "canary-string"; return canaryIdentifier;`,
		"return `canary-template${input.map(x => x)}`;",
		`return {"canary-key": /canary-regex/, canaryProperty: 12345678901234};`,
		`return "\u0063anary-escaped";`,
		`return "canary-invalid`,
		`const \u0063anaryEscaped = "private"; return \u0063anaryEscaped;`,
		`return function() { return "canary-nested"; };`,
		strings.Repeat("canary-long", 500),
	}
	for _, code := range cases {
		profile := observability.ProfileAgentCode(code)
		data, err := json.Marshal(profile)
		require.NoError(t, err)
		require.NotContains(t, string(data), "canary")
		require.NotContains(t, string(data), "12345678901234")
	}
	profile := observability.ProfileAgentCode(
		`for (const x of input) { if(x) return input.filter(y => y).map(z => z); }`,
	)
	require.Equal(t, "source_redacted", profile.Status)
	require.Contains(t, profile.Source, ".filter(")
	require.Contains(t, profile.Outline, "loop")
	require.Contains(t, profile.Outline, "branch")
	require.Contains(t, profile.Methods, "filter")
	require.Contains(t, profile.Methods, "map")
	commented := observability.ProfileAgentCode("return input;\n//# sourceMappingURL=/nonexistent/private-canary.map")
	require.Equal(t, "structural", commented.Status)
	require.Empty(t, commented.Source)
}

func TestAgentCodeSurvivesActualLogger(t *testing.T) {
	t.Parallel()
	var log, exported bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	recorder.EmitCode(t.Context(), observability.AgentEvent{Phase: "script", Operation: "js.run", Outcome: "started"},
		`const canary = "private-memo-canary"; return input.filter(item => item.text === canary).map(item => item.id);`)
	page, err := observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&exported,
		observability.AgentExportOptions{Limit: 10},
	)
	require.NoError(t, err)
	require.Equal(t, 1, page.Records)
	require.NotContains(t, log.String(), "canary")
	require.NotContains(t, exported.String(), "canary")
	require.Contains(t, exported.String(), "source_redacted")
	require.Contains(t, exported.String(), ".filter(")
}

func TestAgentEventBudget(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	recorder, err := observability.NewAgentEvents(
		observability.NewLogger(&log, observability.LogConfig{}),
		uuid.NewString(),
		1,
	)
	require.NoError(t, err)
	for range 300 {
		recorder.Emit(t.Context(), observability.AgentEvent{Phase: "tool", Operation: "read", Outcome: "ok"})
	}
	require.Equal(t, 256, strings.Count(log.String(), "\n"))
	require.Contains(t, log.String(), `\"outcome\":\"limited\"`)
}
