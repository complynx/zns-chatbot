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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptservice"
)

type modernRuntimeTimings struct {
	mu      sync.Mutex
	log     bytes.Buffer
	api     []modernRuntimeAPITiming
	started time.Time
	dropped bool
}

type modernRuntimeAPITiming struct {
	operation string
	at        time.Duration
	elapsed   time.Duration
	status    int
}

func (d *modernRuntimeTimings) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.log.Len()+len(p) <= 64<<10 {
		_, _ = d.log.Write(p)
	} else {
		d.dropped = true
	}
	return len(p), nil
}

func (d *modernRuntimeTimings) report(t *testing.T) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	var exported bytes.Buffer
	page, err := observability.ExportAgentLog(t.Context(), bytes.NewReader(d.log.Bytes()), &exported,
		observability.AgentExportOptions{Limit: 256})
	if err != nil {
		t.Logf("runtime timing export unavailable")
		return
	}
	d.dropped = d.dropped || page.Incomplete
	for line := range strings.SplitSeq(strings.TrimSpace(exported.String()), "\n") {
		var record observability.AgentRecord
		if json.Unmarshal([]byte(line), &record) == nil && record.Outcome != "started" {
			d.dropped = d.dropped || record.Incomplete
			t.Logf("runtime timing phase=%s operation=%s outcome=%s elapsed_ms=%d input_bytes=%d output_bytes=%d",
				record.Phase, record.Operation, record.Outcome, record.ElapsedMS, record.InputBytes, record.OutputBytes)
		}
	}
	for _, call := range d.api {
		t.Logf(
			"runtime API operation=%s status=%d at=%s elapsed=%s",
			call.operation,
			call.status,
			call.at,
			call.elapsed,
		)
	}
	t.Logf("runtime timing incomplete=%t", d.dropped)
}

type modernRuntimeAPITransport struct {
	base    http.RoundTripper
	timings *modernRuntimeTimings
	scope   string
}

func modernRuntimeTimedClient(previous *http.Client, timings *modernRuntimeTimings, scope string) *http.Client {
	client := http.Client{}
	if previous != nil {
		client = *previous
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = modernRuntimeAPITransport{base: transport, timings: timings, scope: scope}
	return &client
}

func (transport modernRuntimeAPITransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		transport.record(request, started, 0)
		return nil, err
	}
	response.Body = &modernRuntimeAPIBody{
		ReadCloser: response.Body,
		finish:     func() { transport.record(request, started, response.StatusCode) },
	}
	return response, nil
}

func (transport modernRuntimeAPITransport) record(request *http.Request, started time.Time, status int) {
	operation := "api.other"
	path := request.URL.Path
	switch {
	case strings.Contains(path, "/orders/"):
		operation = "api.order"
	case strings.HasSuffix(path, "/orders"):
		operation = "api.orders"
	case strings.HasSuffix(path, "/admins"):
		operation = "api.payment_admins"
	case strings.HasPrefix(path, "/v1/order-events/"):
		operation = "api.order_event"
	case strings.HasSuffix(path, "/preferences"):
		operation = "api.preferences"
	case strings.Contains(path, "/refunds"):
		operation = "api.refunds"
	}
	transport.timings.mu.Lock()
	defer transport.timings.mu.Unlock()
	if len(transport.timings.api) < 128 {
		transport.timings.api = append(transport.timings.api, modernRuntimeAPITiming{
			operation: transport.scope + "." + request.Method + "." + operation,
			at:        started.Sub(transport.timings.started),
			elapsed:   time.Since(started),
			status:    status,
		})
	} else {
		transport.timings.dropped = true
	}
}

type modernRuntimeAPIBody struct {
	io.ReadCloser

	once   sync.Once
	finish func()
}

func (body *modernRuntimeAPIBody) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(body.finish)
	return err
}

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
	return installModernRuntimeFixture(t, f.fake.URL, body)
}

func installModernRuntimeFixture(t *testing.T, address string, body []byte) int64 {
	t.Helper()
	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, address+"/lab/model/fixtures", bytes.NewReader(body),
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
