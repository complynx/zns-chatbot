package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsResponsesHeaderSurvivesInvalidBody(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			db := database(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-ID", "req_synthetic_text")
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private invalid body"))
			}))
			defer server.Close()
			model := agent.OpenAI{
				Key:        "synthetic",
				BaseURL:    server.URL,
				HTTP:       server.Client(),
				Accounting: credits.Service{DB: db},
			}
			_, err := model.InformalName(t.Context(), map[string]any{"first_name": "private input"})
			require.Error(t, err)
			var raw []byte
			require.NoError(t, db.QueryRow(t.Context(), `SELECT usage FROM credits.attempts`).Scan(&raw))
			var value credits.Settlement
			require.NoError(t, json.Unmarshal(raw, &value))
			require.Equal(t, "req_synthetic_text", value.Usage.RequestID)
			require.NotContains(t, string(raw), "private")
			require.Equal(t, "unknown", value.Usage.Basis)
		})
	}
}
