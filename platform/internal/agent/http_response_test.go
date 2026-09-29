package agent_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestScriptedModeHTTPContract(t *testing.T) {
	t.Parallel()
	const limit = 64 << 10
	valid := `{"mode":"normal"}`
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"normal", valid, http.StatusOK},
		{"exact limit", strings.Repeat(" ", limit-len(valid)) + valid, http.StatusOK},
		{"over limit", strings.Repeat(" ", limit-len(valid)+1) + valid, http.StatusBadRequest},
		{"unknown field", `{"mode":"normal","extra":true}`, http.StatusBadRequest},
		{"trailing value", valid + ` {}`, http.StatusBadRequest},
		{"trailing malformed", valid + ` !`, http.StatusBadRequest},
		{"invalid mode", `{"mode":"other"}`, http.StatusBadRequest},
		{"wrong type", `{"mode":1}`, http.StatusBadRequest},
		{"null", `null`, http.StatusBadRequest},
		{"empty", ``, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := (&agent.ScriptedServer{}).Handler()
			setup := httptest.NewRecorder()
			handler.ServeHTTP(
				setup,
				httptest.NewRequest(http.MethodPost, "/mode", strings.NewReader(`{"mode":"fail"}`)),
			)
			require.Equal(t, http.StatusOK, setup.Code)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mode", strings.NewReader(test.body)))
			require.Equal(t, test.status, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			if test.status == http.StatusOK {
				require.Equal(t, valid+"\n", response.Body.String())
			} else {
				require.Equal(t, "null\n", response.Body.String())
				plan := httptest.NewRecorder()
				handler.ServeHTTP(plan, httptest.NewRequest(http.MethodPost, "/plan", strings.NewReader(`{}`)))
				require.Equal(
					t,
					http.StatusServiceUnavailable,
					plan.Code,
					"rejected mode must not change fixture state",
				)
				require.Equal(t, "null\n", plan.Body.String())
			}
		})
	}
}

type httpContractModel struct{ failure bool }

func (m httpContractModel) Plan(context.Context, agent.Input) (agent.Plan, error) {
	if m.failure {
		return agent.Plan{}, errors.New("private provider detail")
	}
	return agent.Plan{Text: "<&>", View: "workflow"}, nil
}

func TestModelHTTPResponseContract(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, method, path, body string
		failure                  bool
		status                   int
		response                 string
	}{
		{"health", http.MethodGet, "/healthz", "", false, http.StatusOK, "{\"ok\":true}\n"},
		{"plan", http.MethodPost, "/plan", `{}`, false, http.StatusOK, "{\"text\":\"\\u003c\\u0026\\u003e\",\"view\":\"workflow\"}\n"},
		{"invalid", http.MethodPost, "/plan", `{"unknown":true}`, false, http.StatusBadRequest, "null\n"},
		{"unavailable", http.MethodPost, "/plan", `{}`, true, http.StatusBadGateway, "{\"error\":\"model unavailable\"}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			agent.ModelHandler(httpContractModel{failure: test.failure}).
				ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			require.Equal(t, test.status, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Equal(t, test.response, response.Body.String())
		})
	}
}
