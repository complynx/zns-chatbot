package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/task"
)

func TestExternalAgentToolsRequireLocalUserAndFollowAPIPrivileges(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 74)
	h := prepareHarness(t, p)
	credentials := identity.ClientCredentials{ID: "client", Secret: "fixture"}
	iam, err := identity.New(identity.Options{
		Issuer: p.iam.URL, OrganizationID: "org", BotID: 123,
		LoomAudience: "loom", APIAudience: "api", ActorID: "actor",
		ActorClientCredentials: credentials, ExchangeClientCredentials: credentials,
		IntrospectionClientCredentials: credentials, ExternalAgentAudience: "mcp",
		ExternalAgentIntrospectionCredentials: credentials,
	}, p.iam.Client())
	require.NoError(t, err)
	const secret = "fixture-loom-token-secret"
	server, err := api.New(h.store, iam, &task.Service{}, api.Options{
		ExternalAgentEnabled: true, ExternalAgentTokenSecret: secret, ExternalAgentWrites: true,
		BotID: 123, WebhookSecret: "webhook-secret", OrganizationID: "org",
		ActorID: "actor", Issuer: p.iam.URL, LoomAudience: "loom", APIAudience: "api",
		RegistrationDetails: p.registrationDetails,
	})
	require.NoError(t, err)
	const tokenPath = "/internal/agent/external-agent/token"
	const accountPath = "/internal/agent/account/me"
	const adminDiscoverPath = "/internal/agent/administration/discover"
	const args = `{"protocol_version":1,"operation_id":"loom-execution","run_id":"external-script","arguments":{}}`
	post := func(path, token, body string, key ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if len(key) == 1 {
			r.Header.Set("X-Loom-External-Write-Key", key[0])
		}
		out := httptest.NewRecorder()
		server.InternalHandler().ServeHTTP(out, r)
		return out
	}
	mcpToken := func(subject string, admin bool) string {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.adminRole = admin
		p.users[subject] = map[string]any{
			"userId": subject, "state": "USER_STATE_ACTIVE",
			"details": map[string]string{"resourceOwner": "org"},
			"human":   map[string]any{},
		}
		return p.tokenForAudience(subject, []string{"mcp"}, false)
	}
	mint := func(token string) *httptest.ResponseRecorder {
		return post(tokenPath, secret, `{"protocol_version":1,"subject_token":"`+token+`"}`)
	}
	unknown := mcpToken("unknown", false)
	require.Equal(t, http.StatusForbidden, mint(unknown).Code)
	for _, tc := range []struct {
		subject string
		admin   bool
		id      int64
	}{
		{"regular-owner", false, 501},
		{"administrator", true, 502},
	} {
		owner := scopedAccount(t, h, 123, tc.id, tc.subject)
		rawMCP := mcpToken(tc.subject, tc.admin)
		require.Equal(t, http.StatusUnauthorized, post(accountPath, rawMCP, args).Code)
		response := mint(rawMCP)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var receipt struct {
			Token string `json:"access_token"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
		account := post(accountPath, receipt.Token, args)
		require.Equal(t, http.StatusOK, account.Code, account.Body.String())
		require.Contains(t, account.Body.String(), owner.ID.String())
		discovery := post(adminDiscoverPath, receipt.Token, "{}")
		require.Equal(t, http.StatusOK, discovery.Code, discovery.Body.String())
		if tc.admin {
			assertExternalAgentAdminRead(t, discovery.Body.Bytes())
			settings := post("/internal/agent/administration/settings", receipt.Token, args)
			require.Equal(t, http.StatusOK, settings.Code, settings.Body.String())
			revoke := post("/internal/admin/users/"+owner.ID.String()+"/revoke", receipt.Token, "")
			require.Equal(t, http.StatusForbidden, revoke.Code, revoke.Body.String())
			require.Contains(t, revoke.Body.String(), "requires an authorized tool route")
		} else {
			require.JSONEq(t, `{"methods":[]}`, discovery.Body.String())
			profile, profileErr := h.store.GetProfile(t.Context(), db.ProfileScope{
				BotID: owner.BotID, Subject: owner.Subject, Organization: owner.Organization, UserID: owner.ID,
			})
			require.NoError(t, profileErr)
			id := uuid.NewString()
			mutation := fmt.Sprintf(`{"protocol_version":1,"operation_id":%q,"idempotency_key":%q,`+
				`"confirmation":{"mode":"none"},"arguments":{"expected_version":%d,"bot_language":"ru"}}`, id, id, profile.Version)
			const path = "/internal/agent/profile/set_language"
			require.Equal(t, http.StatusForbidden, post(path, receipt.Token, mutation).Code)
			require.Equal(t, http.StatusForbidden, post(path, receipt.Token, mutation, "wrong").Code)
			saved := post(path, receipt.Token, mutation, secret)
			require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
			require.Contains(t, saved.Body.String(), `"status":"succeeded"`)
			passport := uuid.NewString()
			const canary = "SYNTHETIC-MCP-PASSPORT-501"
			body := fmt.Sprintf(`{"protocol_version":1,"operation_id":%q,"idempotency_key":%q,`+
				`"confirmation":{"mode":"none"},"arguments":{"expected_version":0,"passport_number":%q}}`,
				passport, passport, canary)
			const passportPath = "/internal/agent/registration_details/set_passport_number"
			require.Equal(t, http.StatusForbidden, post(passportPath, receipt.Token, body).Code)
			saved = post(passportPath, receipt.Token, body, secret)
			require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
			require.Contains(t, saved.Body.String(), `"passport_number_present":true`)
			require.NotContains(t, saved.Body.String(), canary)
			status := post("/internal/agent/registration_details/operation", receipt.Token,
				fmt.Sprintf(`{"protocol_version":1,"operation_id":%q}`, passport), secret)
			require.Equal(t, http.StatusOK, status.Code, status.Body.String())
			require.Contains(t, status.Body.String(), `"passport_number_present":true`)
			require.NotContains(t, status.Body.String(), canary)
		}
	}
}

func assertExternalAgentAdminRead(t *testing.T, raw []byte) {
	t.Helper()
	var document struct {
		Methods []map[string]any `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &document))
	for _, method := range document.Methods {
		if method["id"] == "settings" {
			require.Equal(t, "read", method["effect"])
			require.Equal(t, true, method["external_agent"])
			return
		}
	}
	t.Fatal("admin settings missing from discovery")
}
