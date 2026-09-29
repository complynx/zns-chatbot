package main

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

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
)

func TestConfiguredScriptsExecuteHostTools(t *testing.T) {
	t.Parallel()
	telemetry, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	socket := filepath.Join(t.TempDir(), "s")
	b := &bot.Bot{}
	closeClient, err := configureBotScripts(
		b,
		config.Config{Script: config.Script{Enabled: true, Socket: socket}},
		telemetry,
	)
	require.NoError(t, err)
	defer closeClient()
	executor, ok := b.Scripts.(interface {
		Execute(context.Context, scriptclient.Request, []scriptclient.Tool, scriptclient.Callback) (json.RawMessage, error)
	})
	require.True(t, ok, "configured wrapper must expose the host-tool execution API")
	name := "scriptworker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	output, err := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "../scriptworker").CombinedOutput()
	require.NoError(t, err, "%s", output)
	service, err := scriptservice.New(executable)
	require.NoError(t, err)
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: service}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	calls := make(chan scriptclient.ToolCall, 1)
	result, err := executor.Execute(t.Context(), scriptclient.Request{
		Code: `return await tools.workflow.get({});`, Input: json.RawMessage(`null`),
	}, []scriptclient.Tool{{Name: "workflow.get"}}, func(_ context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
		calls <- call
		return json.RawMessage(`{"version":7}`), nil
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"version":7}`, string(result))
	require.Len(t, calls, 1)
	require.Equal(t, "workflow.get", (<-calls).Name)
}
