package scriptservice_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
)

func TestExecuteThroughIsolatedProcess(t *testing.T) {
	t.Parallel()
	name := "scriptworker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	output, err := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "../../cmd/scriptworker").
		CombinedOutput()
	require.NoError(t, err, "%s", output)
	service, err := scriptservice.New(executable)
	require.NoError(t, err)
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: service}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	client, err := scriptclient.New(socket)
	require.NoError(t, err)
	defer client.Close()
	calls := make(chan string, 8)
	result, err := client.Execute(
		t.Context(),
		scriptclient.Request{
			Code:  `const help=tools.orders.get.$help();const a=await tools.orders.get({id:1});const b=await tools.orders.get({id:2});return {sum:a.value+b.value,name:help.name};`,
			Input: json.RawMessage(`null`),
		},
		[]scriptclient.Tool{{Name: "orders.get"}},
		func(_ context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			if call.Name == "$help" {
				return json.RawMessage(`{"name":"orders.get"}`), nil
			}
			calls <- call.Name
			return json.RawMessage(`{"value":7}`), nil
		},
	)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	for range 2 {
		require.Equal(t, "orders.get", <-calls)
	}
	require.JSONEq(t, `{"sum":14,"name":"orders.get"}`, string(result))
}
