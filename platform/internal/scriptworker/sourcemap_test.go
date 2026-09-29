package scriptworker_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/sobek"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestEvaluatorsDoNotLoadAmbientSourceMaps(t *testing.T) {
	t.Parallel()
	const marker = "SCRIPT_SANDBOX_SOURCE_MAP_MARKER"
	mapPath := filepath.Join(t.TempDir(), "source.map")
	require.NoError(
		t,
		os.WriteFile(mapPath, []byte(`{"version":3,"sources":["`+marker+`"],"names":[],"mappings":"AAAA"}`), 0600),
	)
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	relativePath, err := filepath.Rel(workingDirectory, mapPath)
	require.NoError(t, err)
	comment := "\n//# sourceMappingURL=" + filepath.ToSlash(relativePath)
	evalSource, err := json.Marshal("new Error().stack" + comment)
	require.NoError(t, err)
	functionSource, err := json.Marshal("return new Error().stack;" + comment)
	require.NoError(t, err)
	// Validate the QA-owned fixture against the pinned parser's unsafe default.
	control, err := sobek.New().RunString("eval(" + string(evalSource) + ")")
	require.NoError(t, err)
	require.Contains(t, control.String(), marker)
	for name, code := range map[string]string{
		"direct":   "return new Error().stack;" + comment,
		"eval":     "return eval(" + string(evalSource) + ");",
		"Function": "return Function(" + string(functionSource) + ")();",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, evaluateErr := scriptworker.Evaluate(
				t.Context(),
				scriptworker.Request{Code: code, Input: json.RawMessage(`null`)},
			)
			require.NoError(t, evaluateErr)
			require.NotContains(t, string(data), marker)
			data, executeErr := scriptworker.Execute(
				t.Context(),
				scriptprotocol.ExecuteRequest{Code: code, Input: json.RawMessage(`null`)},
				nil,
			)
			require.NoError(t, executeErr)
			require.NotContains(t, string(data), marker)
		})
	}
}
