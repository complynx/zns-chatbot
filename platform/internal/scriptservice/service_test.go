package scriptservice_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestIsolatedProcessEvaluation(t *testing.T) {
	t.Parallel()
	name := "scriptworker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "../../cmd/scriptworker")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	service, err := scriptservice.New(executable)
	require.NoError(t, err)
	for _, tc := range []struct {
		code     string
		succeeds bool
	}{
		{"return input.reduce((sum,n)=>sum+n,0)", true},
		{"return typeof process + ':' + typeof fetch", true},
		{"while(true) {}", false},
		{"return input.reduce((sum,n)=>sum+n,0)", true},
	} {
		body, marshalErr := json.Marshal(scriptworker.Request{Code: tc.code, Input: json.RawMessage(`[10,20,12]`)})
		require.NoError(t, marshalErr)
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/evaluate", bytes.NewReader(body))
		response := httptest.NewRecorder()
		service.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		var result scriptworker.Response
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		if tc.succeeds {
			require.Empty(t, result.Error)
			if tc.code == "return typeof process + ':' + typeof fetch" {
				require.JSONEq(t, `"undefined:undefined"`, string(result.Result))
			} else {
				require.JSONEq(t, `42`, string(result.Result))
			}
		} else {
			require.NotEmpty(t, result.Error)
		}
	}
}
