package browserauth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
)

func TestRoutingErrorsUseAuthenticationEnvelope(t *testing.T) {
	t.Parallel()
	const origin = "https://browser.example"
	service := &browserauth.Service{Origin: origin}
	for _, check := range []struct {
		method, path, allow, code string
		status                    int
	}{
		{http.MethodGet, "/miniapp/auth/start", http.MethodPost, "method_not_allowed", http.StatusMethodNotAllowed},
		{http.MethodPost, "/miniapp/auth/status", "GET, HEAD", "method_not_allowed", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/miniapp/auth/cancel", http.MethodPost, "method_not_allowed", http.StatusMethodNotAllowed},
		{http.MethodGet, "/miniapp/auth/missing", "", "not_found", http.StatusNotFound},
	} {
		request := httptest.NewRequest(check.method, origin+check.path, nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("X-Browser-Auth", "1")
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, request)
		assert.Equal(t, check.status, response.Code, check.path)
		assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		assert.Equal(t, check.allow, response.Header().Get("Allow"))
		assert.JSONEq(t, `{"code":"`+check.code+`"}`, response.Body.String())
	}
}
