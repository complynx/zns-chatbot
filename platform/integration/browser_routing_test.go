package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
)

func TestBrowserRoutingRejectsNoncanonicalPaths(t *testing.T) {
	t.Parallel()
	const origin = "https://browser.example"
	service := &browserauth.Service{Origin: origin}
	gateway := (miniapp.Gateway{BrowserAuth: service}).Handler()
	outer := http.NewServeMux()
	outer.Handle("/miniapp/", gateway)
	for name, handler := range map[string]http.Handler{
		"service": service.Handler(), "gateway": gateway, "application": browserauth.GuardRouting(outer),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, target := range []string{"/miniapp/auth//check", "/miniapp/auth/./check", "/miniapp/auth/../check"} {
				request := httptest.NewRequest(http.MethodGet, origin+target, nil)
				request.Header.Set("Origin", origin)
				request.Header.Set("X-Browser-Auth", "1")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				assert.Equal(t, http.StatusNotFound, response.Code, target)
				assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
				assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				assert.Empty(t, response.Header().Get("Location"))
				assert.JSONEq(t, `{"code":"not_found"}`, response.Body.String())
			}
		})
	}
}
