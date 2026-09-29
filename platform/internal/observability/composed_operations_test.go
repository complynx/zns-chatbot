package observability_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestComposedToolNamesSurviveDiagnosticExport(t *testing.T) {
	t.Parallel()
	names := []string{
		"models.effective", "models.own.get", "models.own.set",
		"models.others.get", "models.others.set", "models.global.get", "models.global.set", "models.grants.set",
		"massage.book", "massage.cancel", "massage.practitioner.instant", "massage.practitioner.configure",
		"broadcasts.review", "broadcasts.show", "profile.set", "profile.history", "preferences.setLanguage",
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
		observability.AgentEvent{Phase: "tool", Operation: "profile.set.private-secret", Outcome: "ok"},
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

func TestPublicOperationExceptionKeepsSecretPrecedence(t *testing.T) {
	t.Parallel()
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		t.Run(level.String(), func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			logger := observability.NewLogger(&output, observability.LogConfig{
				Level: level, Secrets: []string{"models.own.set"},
			})
			logger.Log(t.Context(), level, "diagnostic", "sample",
				"models.own.get models.own.set models.own.private_canary eyJheader.eyJpayload.signature")
			require.Contains(t, output.String(), "models.own.get")
			require.NotContains(t, output.String(), "models.own.set")
			require.NotContains(t, output.String(), "private_canary")
			require.NotContains(t, output.String(), "signature")
		})
	}
}
