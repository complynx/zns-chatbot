package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

func TestMiniAppRoutesAreOptInAndServeOnlyBoundedPublicAssets(t *testing.T) {
	t.Parallel()
	disabled := newAPI(t, &intake{})
	response := httptest.NewRecorder()
	disabled.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app/", nil))
	require.Equal(t, http.StatusNotFound, response.Code)
	options := api.Options{
		BotID:           42,
		WebhookSecret:   "test-secret",
		OrganizationID:  "org",
		ActorID:         "actor",
		Issuer:          "https://issuer.invalid",
		LoomAudience:    "loom",
		MiniAppOrigin:   "https://app.example.invalid",
		MiniAppBotToken: "42:test-token",
	}
	enabled, err := api.New(&db.Store{}, &identity.Client{}, &intake{}, options)
	require.NoError(t, err)
	for _, path := range []string{"/app/", "/app/app.js", "/app/orders.js", "/app/meals.js", "/app/pairs.js", "/app/avatars.js", "/app/avatar-admin.js", "/app/assignments.js", "/app/order-proof.js", "/app/payment-reviews.js", "/app/order-reviews.js", "/app/massage.js", "/app/massage-staff.js", "/app/massage-admin.js", "/app/app.css"} {
		response = httptest.NewRecorder()
		enabled.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code, path)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		require.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
		require.Contains(t, response.Header().Get("Content-Security-Policy"), "default-src 'none'")
		require.Contains(t, response.Header().Get("Content-Security-Policy"), "img-src 'self' blob:")
		require.NotContains(t, response.Body.String(), "test-token")
	}
	for _, path := range []string{"/app/private", "/app/miniapp_ui/", "/app/miniapp.go"} {
		response = httptest.NewRecorder()
		enabled.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, response.Code)
	}
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/app/me", nil)
	request.Header.Set("Cookie", "session=untrusted")
	enabled.PublicHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code, "Cookies never establish application identity")
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/app/session/bootstrap",
		strings.NewReader(`{"init_data":"invalid","bootstrap_nonce":"`+strings.Repeat("A", 43)+`"}`))
	request.Header.Set("Origin", options.MiniAppOrigin)
	request.Header.Set("Content-Type", "application/json")
	enabled.PublicHandler().ServeHTTP(response, request)
	require.Equal(
		t,
		http.StatusUnauthorized,
		response.Code,
		"A forged proof is rejected before the uninitialized DB or IAM can be reached",
	)
	options.MiniAppBotToken = "43:foreign-bot"
	_, err = api.New(&db.Store{}, &identity.Client{}, &intake{}, options)
	require.Error(t, err)
}

func TestMiniAppLocalHTTPOriginRequiresExplicitAPIOptIn(t *testing.T) {
	t.Parallel()
	options := api.Options{
		BotID:           42,
		WebhookSecret:   "test-secret",
		OrganizationID:  "org",
		ActorID:         "actor",
		Issuer:          "https://issuer.invalid",
		LoomAudience:    "loom",
		MiniAppOrigin:   "http://localhost:18382",
		MiniAppBotToken: "42:test-token",
	}
	_, err := api.New(&db.Store{}, &identity.Client{}, &intake{}, options)
	require.ErrorContains(t, err, "trusted origin")
	options.MiniAppAllowLocalHTTP = true
	enabled, err := api.New(&db.Store{}, &identity.Client{}, &intake{}, options)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/app/session/bootstrap", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://complynx.net")
	enabled.PublicHandler().ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code, "Launch host never becomes the trusted session origin")
	options.MiniAppOrigin = "http://evil.invalid"
	_, err = api.New(&db.Store{}, &identity.Client{}, &intake{}, options)
	require.Error(t, err)
}
