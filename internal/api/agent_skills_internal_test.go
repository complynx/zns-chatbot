package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillsManifestIsExplicitOptIn(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		server := &Server{options: Options{SkillsEnabled: enabled}}
		output := httptest.NewRecorder()
		server.manifest(output, nil)
		var manifest map[string]any
		require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
		if !enabled {
			require.Equal(t, agentContractRevision, manifest["revision"])
			require.NotContains(t, output.Body.String(), `"skills":`)
			require.NotContains(t, output.Body.String(), `"visibility":`)
			continue
		}
		require.Equal(t, agentContractRevision+"-skills-1", manifest["revision"])
		require.Contains(t, output.Body.String(), `"skills":`)
		require.Contains(t, output.Body.String(), `"name":"profile-name"`)
	}
}

func TestSkillsDiscoveryPreservesAuthorizedMethodSet(t *testing.T) {
	t.Parallel()
	server := &Server{options: Options{SkillsEnabled: true}}
	for _, methods := range [][]map[string]any{
		{},
		{{"id": "get", manifestPermissions: []string{"existing.permission"}, manifestEffect: effectRead}},
	} {
		response, err := server.skillDiscoveryResult("zinusya_profile", methods)
		require.NoError(t, err)
		var decoded struct {
			Methods []map[string]any `json:"methods"`
		}
		require.NoError(t, json.Unmarshal(response.Body, &decoded))
		require.Len(t, decoded.Methods, len(methods))
		if len(methods) > 0 {
			require.Equal(t, "get", decoded.Methods[0]["id"])
			require.Equal(t, []any{"existing.permission"}, decoded.Methods[0][manifestPermissions])
			require.Equal(t, effectRead, decoded.Methods[0][manifestEffect])
			require.Contains(t, decoded.Methods[0], "skills")
		}
	}
}

func TestSkillsDisabledDiscoveryKeepsLegacyWire(t *testing.T) {
	t.Parallel()
	server := &Server{}
	methods := []map[string]any{{"id": "get", manifestPermissions: []string{}}}
	want, err := discoveryResult(methods)
	require.NoError(t, err)
	got, err := server.skillDiscoveryResult("zinusya_profile", methods)
	require.NoError(t, err)
	require.Equal(t, want.Body, got.Body)
}

func TestSkillsAnnotatesBothStaticMethodRepresentations(t *testing.T) {
	t.Parallel()
	server := &Server{options: Options{SkillsEnabled: true}}
	for _, methods := range []any{
		[]any{map[string]any{"id": "get"}},
		[]map[string]any{{"id": "get"}},
	} {
		modules := []any{map[string]any{"id": "zinusya_profile", manifestMethods: methods}}
		encoded, err := json.Marshal(server.skillModules(modules))
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"name":"profile-name"`)
	}
}

func TestSkillsDoesNotChangeAuthorityOrExecutionContracts(t *testing.T) {
	t.Parallel()
	manifests := make([]map[string]any, 0, 2)
	for _, enabled := range []bool{false, true} {
		server := &Server{options: Options{SkillsEnabled: enabled}}
		output := httptest.NewRecorder()
		server.manifest(output, nil)
		var manifest map[string]any
		require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
		manifest["revision"] = agentContractRevision
		for _, value := range manifest["modules"].([]any) {
			module := value.(map[string]any)
			delete(module, "visibility")
			methods, _ := module[manifestMethods].([]any)
			for _, method := range methods {
				card := method.(map[string]any)
				delete(card, "visibility")
				delete(card, "skills")
			}
		}
		manifests = append(manifests, manifest)
	}
	require.Equal(t, manifests[0], manifests[1])
}
