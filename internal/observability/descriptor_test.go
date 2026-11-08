package observability_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestDescriptorStateIsIndependentAndContainsNoJournalIdentity(t *testing.T) {
	t.Parallel()
	telemetry, err := observability.New(t.Context(), observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(t.Context())) })
	require.NoError(t, telemetry.ObserveDescriptor(true, func() (string, int64) { return "blocked", 2 }))
	response := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	raw := response.Body.String()
	require.Contains(t, raw, "zinusya_descriptor_enabled 1")
	require.Contains(t, raw, "zinusya_descriptor_state{state=\"blocked\"} 1")
	require.Contains(t, raw, "zinusya_descriptor_state{state=\"running\"} 0")
	require.Contains(t, raw, "zinusya_descriptor_blocked 2")
	require.NotContains(t, raw, "journal=")
}
