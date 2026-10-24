package api_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

func prelinkAPI(t *testing.T) (*api.Server, string) {
	t.Helper()
	iam, err := identity.New(identity.Options{
		BotID:                          42,
		Issuer:                         "https://iam.invalid",
		OrganizationID:                 "org",
		LoomAudience:                   "loom",
		APIAudience:                    "api",
		ActorID:                        "actor",
		ActorClientCredentials:         identity.ClientCredentials{ID: "actor-client", Secret: "actor-secret"},
		ExchangeClientCredentials:      identity.ClientCredentials{ID: "exchange", Secret: "exchange-secret"},
		IntrospectionClientCredentials: identity.ClientCredentials{ID: "introspection", Secret: "introspection-secret"},
	}, nil)
	require.NoError(t, err)
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	server, err := api.New(&db.Store{}, iam, &intake{}, api.Options{
		BotID:          42,
		WebhookSecret:  "webhook-secret",
		OrganizationID: "org",
		ActorID:        "actor",
		Issuer:         "https://iam.invalid",
		LoomAudience:   "loom",
		APIAudience:    "api",
		TelegramPrelink: api.TelegramPrelinkOptions{
			Issuer: "https://provider.invalid", JWKSURL: "https://provider.invalid/keys", Audience: "zinusya-prelink",
			ProviderID: "provider", ServiceBearer: key,
		},
	})
	require.NoError(t, err)
	return server, key
}

func TestTelegramPrelinkPrivateBoundaryRejectsBeforeDB(t *testing.T) {
	t.Parallel()
	s, key := prelinkAPI(t)
	server := httptest.NewServer(s.InternalHandler())
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		name, auth, body, origin, media string
		code                            int
	}{
		{name: "no credentials", body: `{"assertion":"a.b.c"}`, code: 401},
		{name: "wrong credential", auth: "Bearer wrong", body: `{"assertion":"a.b.c"}`, code: 401},
		{name: "browser session", auth: "ZinusyaSession " + key, code: 401},
		{name: "origin", auth: "Bearer " + key, origin: "https://app.invalid", code: 403},
		{name: "content type", auth: "Bearer " + key, media: "text/plain", code: 415},
		{name: "duplicate assertion", auth: "Bearer " + key, body: `{"assertion":"a","assertion":"b"}`, code: 400},
		{name: "wrong field case", auth: "Bearer " + key, body: `{"Assertion":"a.b.c"}`, code: 400},
		{name: "target user", auth: "Bearer " + key, body: `{"assertion":"a.b.c","user_id":"victim"}`, code: 400},
		{name: "extra value", auth: "Bearer " + key, body: `{"assertion":"a.b.c"} {}`, code: 400},
		{name: "malformed proof", auth: "Bearer " + key, body: `{"assertion":"a.b.c"}`, code: 403},
		{name: "oversized", auth: "Bearer " + key, body: `{"assertion":"` + strings.Repeat("x", 25<<10) + `"}`, code: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				server.URL+"/internal/identity/telegram-prelink",
				strings.NewReader(tc.body),
			)
			require.NoError(t, err)
			req.Header.Set("Authorization", tc.auth)
			req.Header.Set("Content-Type", "application/json")
			if tc.media != "" {
				req.Header.Set("Content-Type", tc.media)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			response, err := server.Client().Do(req)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, tc.code, response.StatusCode, string(body))
			require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
			require.NotContains(t, string(body), key)
			require.NotContains(t, string(body), "a.b.c")
		})
	}
}

func TestTelegramPrelinkIsNotPublicAndDisabledByDefault(t *testing.T) {
	t.Parallel()
	s, _ := prelinkAPI(t)
	response := httptest.NewRecorder()
	s.PublicHandler().
		ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/internal/identity/telegram-prelink", nil))
	require.Equal(t, http.StatusNotFound, response.Code)
	disabled := newAPI(t, &intake{})
	response = httptest.NewRecorder()
	disabled.InternalHandler().
		ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/internal/identity/telegram-prelink", nil))
	require.Equal(t, http.StatusNotFound, response.Code)
}
