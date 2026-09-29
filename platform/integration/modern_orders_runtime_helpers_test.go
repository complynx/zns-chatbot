package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
)

// The supervisor starts a new external worker process for each execution.
func startModernRuntimeWorker(t *testing.T) *scriptclient.Client {
	t.Helper()
	name := "scriptworker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	output, err := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "../cmd/scriptworker").
		CombinedOutput()
	require.NoError(t, err, "%s", output)
	service, err := scriptservice.New(executable)
	require.NoError(t, err)
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: service}
	t.Cleanup(func() { assert.NoError(t, server.Close()) })
	go func() { _ = server.Serve(listener) }()
	client, err := scriptclient.New(socket)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}

func queueModernRuntimePlan(
	t *testing.T,
	f *fixture,
	user int64,
	language, text string,
	plans ...agent.Plan,
) int64 {
	t.Helper()
	steps := make([]map[string]any, 0, len(plans))
	for _, plan := range plans {
		steps = append(steps, map[string]any{"expect": map[string]any{"text": text}, "plan": plan})
	}
	body, err := json.Marshal(map[string]any{
		"input": map[string]any{"user": user, "language_code": language, "text": text},
		"steps": steps,
	})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, f.fake.URL+"/lab/model/fixtures", bytes.NewReader(body),
	)
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(body))
	var installed struct {
		UpdateID int64 `json:"update_id"`
	}
	require.NoError(t, json.Unmarshal(body, &installed))
	require.Positive(t, installed.UpdateID)
	return installed.UpdateID
}
