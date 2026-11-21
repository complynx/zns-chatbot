package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestExternalAgentManifestPermissionRequiresOptIn(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		server := &Server{options: Options{ExternalAgentEnabled: enabled}}
		output := httptest.NewRecorder()
		server.manifest(output, nil)
		var manifest struct {
			Revision string `json:"revision"`
			Caller   struct {
				Permissions []string `json:"permissions"`
			} `json:"caller"`
		}
		require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
		if enabled {
			require.Contains(t, manifest.Caller.Permissions, loom.ExternalAgentProfilePermission)
			require.Equal(t, agentContractRevision+"-mcp-1", manifest.Revision)
		} else {
			require.NotContains(t, manifest.Caller.Permissions, loom.ExternalAgentProfilePermission)
			require.Equal(t, agentContractRevision, manifest.Revision)
		}
	}
}

func TestExternalAgentCatalogAndReadPathsSharePolicy(t *testing.T) {
	t.Parallel()
	server := &Server{options: Options{
		ExternalAgentEnabled:     true,
		ExternalAgentTokenSecret: "fixture-token-secret",
		RegistrationDetails:      &registrationdetails.Protector{},
	}}
	paths := externalAgentReadPaths(server.manifestModules())
	output := httptest.NewRecorder()
	server.manifest(output, nil)
	var manifest struct {
		ExternalAgentTokens struct {
			Path string `json:"path"`
		} `json:"external_agent_tokens"`
		Modules []struct {
			ID      string           `json:"id"`
			Methods []map[string]any `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
	require.Equal(t, externalAgentTokenPath, manifest.ExternalAgentTokens.Path)
	for _, module := range manifest.Modules {
		methods := module.Methods
		if len(methods) == 0 {
			discovery, err := server.skillDiscoveryResult(module.ID, externalAgentDiscoveryCards(module.ID))
			require.NoError(t, err)
			var document struct {
				Methods []map[string]any `json:"methods"`
			}
			require.NoError(t, json.Unmarshal(discovery.Body, &document))
			methods = document.Methods
		}
		for _, method := range methods {
			path, ok := method[manifestPath].(string)
			require.True(t, ok)
			require.Equal(t, method[externalAgentMark] == true, paths[path], path)
			if method[externalAgentMark] == true {
				require.Equal(t, effectRead, method[manifestEffect], path)
				require.Empty(t, method[manifestMIMETypes], path)
			}
		}
	}
	for _, path := range []string{
		"/internal/agent/account/me", "/internal/agent/orders/list", "/internal/agent/administration/tasks",
		"/internal/agent/pairs/discover", "/internal/agent/administration/discover",
	} {
		require.Contains(t, paths, path)
	}
	require.NotContains(t, paths, externalAgentTokenPath)
	require.NotContains(t, paths, "/internal/agent/files/fetch")
	require.NotContains(t, paths, "/internal/agent/administration/apply_history")
}

func TestExternalAgentTokenServiceRequiresTrustedCallerBeforeDecoding(t *testing.T) {
	t.Parallel()
	const secret = "fixture-token-secret"
	server := &Server{internal: http.NewServeMux(), options: Options{ExternalAgentTokenSecret: secret}}
	server.registerExternalAgentTokenService()
	for _, tc := range []struct {
		bearer string
		status int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer wrong-secret", http.StatusUnauthorized},
		{"Bearer " + secret, http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPost, externalAgentTokenPath, strings.NewReader("bad JSON"))
		request.Header.Set("Authorization", tc.bearer)
		output := httptest.NewRecorder()
		server.InternalHandler().ServeHTTP(output, request)
		require.Equal(t, tc.status, output.Code)
		require.Equal(t, "no-store", output.Header().Get("Cache-Control"))
	}
}

func TestExternalAgentManifestProtectsPassportWrite(t *testing.T) {
	t.Parallel()
	s := &Server{options: Options{
		ExternalAgentEnabled: true, ExternalAgentWrites: true,
		ExternalAgentTokenSecret: "fixture-token-secret", RegistrationDetails: &registrationdetails.Protector{},
	}}
	output := httptest.NewRecorder()
	s.manifest(output, nil)
	var manifest struct {
		Modules []struct {
			ID      string           `json:"id"`
			Methods []map[string]any `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
	paths := externalAgentWritePaths(s.manifestModules())
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_registration_details" {
			continue
		}
		for _, method := range module.Methods {
			if method["id"] == "set_passport_number" {
				require.Equal(t, true, method[externalAgentMark])
				require.Equal(t, true, method["external_agent_protected_arguments"])
				require.Contains(t, paths, method[manifestPath])
				require.Contains(t, paths, method[manifestStatusPath])
				return
			}
		}
	}
	t.Fatal("protected passport write missing from the MCP manifest")
}
