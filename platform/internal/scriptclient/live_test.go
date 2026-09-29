package scriptclient_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// TestRealUnixWorker is opt-in: the worker must be in its networkless,
// hard-memory-limited container, and the client only receives the IPC socket.
func TestRealUnixWorker(t *testing.T) {
	t.Parallel()
	socket := os.Getenv("SCRIPT_TEST_SOCKET")
	if socket == "" {
		t.Skip("SCRIPT_TEST_SOCKET enables isolated worker acceptance")
	}
	client, err := scriptclient.New(socket)
	require.NoError(t, err)
	defer client.Close()
	for _, code := range []string{
		"return input.reduce((sum,n)=>sum+n,0)",
		"while (true) {}",
		"return new Proxy({}, {ownKeys(){while(true){}}})",
		"return input.reduce((sum,n)=>sum+n,0)",
		"return {network:typeof fetch, process:typeof process, require:typeof require}",
	} {
		result, runErr := client.Evaluate(
			t.Context(), scriptclient.Request{Code: code, Input: json.RawMessage(`[10,20,12]`)},
		)
		switch code {
		case "return input.reduce((sum,n)=>sum+n,0)":
			require.NoError(t, runErr)
			require.JSONEq(t, `42`, string(result))
		case "return {network:typeof fetch, process:typeof process, require:typeof require}":
			require.NoError(t, runErr)
			require.JSONEq(t, `{"network":"undefined","process":"undefined","require":"undefined"}`, string(result))
		default:
			require.Error(t, runErr)
		}
	}
}
