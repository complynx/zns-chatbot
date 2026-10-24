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

func TestMiniAppPaymentsRequireSessionBeforeStoreOrIAM(t *testing.T) {
	t.Parallel()
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
	server, err := api.New(&db.Store{}, &identity.Client{}, &intake{}, options)
	require.NoError(t, err)
	id := "00000000-0000-0000-0000-000000000001"
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/app/pass-payments/targets", ""},
		{http.MethodGet, "/app/pass-payments/recent", ""},
		{http.MethodGet, "/app/pass-payments/events/" + id + "/files/" + id + "/content", ""},
		{http.MethodGet, "/app/payment-reviews/events", ""},
		{http.MethodGet, "/app/payment-reviews/events/" + id + "/evidence", ""},
		{
			http.MethodPost,
			"/app/pass-payments/events/" + id + "/evidence",
			`{"operation_id":"qa-payment","pass_id":"` +
				id +
				`","file_id":"` +
				id +
				`","amount_minor":3000,"purpose":"initial"}`,
		},
	}
	for _, route := range routes {
		for _, auth := range []string{"", "Bearer untrusted", "ZinusyaSession malformed"} {
			t.Run(route.method+route.path+"/"+auth, func(t *testing.T) {
				t.Parallel()
				request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				request.Header.Set("Authorization", auth)
				request.Header.Set("Cookie", "session=untrusted")
				request.Header.Set("Origin", options.MiniAppOrigin)
				if route.method == http.MethodPost {
					request.Header.Set("Content-Type", "application/json")
				}
				response := httptest.NewRecorder()
				server.PublicHandler().ServeHTTP(response, request)
				require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "reopen the Mini App")
				require.Empty(t, response.Header().Get("Retry-After"))
				require.NotContains(t, response.Body.String(), "untrusted")
			})
		}
	}
}
