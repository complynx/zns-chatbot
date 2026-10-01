package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

type modelControlFixture struct {
	fake    *Fake
	data    *httptest.Server
	control *httptest.Server
	key     string
}

func newModelControlFixture(t *testing.T) modelControlFixture {
	t.Helper()
	f, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	key := strings.Repeat("s", delayMinimumKeyBytes)
	f.delay = &editDelay{
		key:          key,
		ctx:          t.Context(),
		modelControl: f.modelControl,
		dataSlots:    make(chan struct{}, delayDataConnections),
	}
	data := httptest.NewServer(f.Handler())
	control := httptest.NewServer(http.HandlerFunc(f.delay.control))
	t.Cleanup(data.Close)
	t.Cleanup(control.Close)
	return modelControlFixture{f, data, control, key}
}

func (f modelControlFixture) request(t *testing.T, method, path, body, key string, control bool) (int, string) {
	t.Helper()
	base := f.data.URL
	if control {
		base = f.control.URL
	}
	r, err := http.NewRequestWithContext(t.Context(), method, base+path, strings.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", key)
	response, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(raw)
}

func (f modelControlFixture) install(t *testing.T, owner string, update int64, mode string) {
	t.Helper()
	value := modelFixtureInstall{
		Owner:    owner,
		UpdateID: update,
		Steps:    []modelFixtureStep{fixtureStep("private marker")},
	}
	if mode != "" {
		value.Hold = &modelFixtureHoldSetup{Mode: mode}
	}
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	status, body := f.request(t, http.MethodPost, "/lab/model/fixtures", string(raw), f.key, false)
	require.Equal(t, http.StatusCreated, status, body)
}

const modelControlScope = "?owner=alice&update_id=9&turn=0"

func TestModelControlHoldExpiresWithoutDetachedWaiter(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t)
	f.install(t, "alice", 9, modelHoldAfter)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/lab/model/plan",
		strings.NewReader(`{"text":"private marker"}`),
	)
	request.Header.Set("X-Sandbox", "1")
	request.Header.Set("X-Sandbox-Actor", "alice")
	request.Header.Set("X-Sandbox-Update", "9")
	request.Header.Set("X-Sandbox-Turn", "0")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	started := time.Now()
	go func() { f.fake.Handler().ServeHTTP(response, request); close(done) }()
	f.state(t, "held_process_local")
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		t.Fatal("hold did not terminate")
	}
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.GreaterOrEqual(t, time.Since(started), modelHoldTimeout)
	f.state(t, "hold_expired")
}

func (f modelControlFixture) state(t *testing.T, phase string) modelFixtureControlState {
	t.Helper()
	var value modelFixtureControlState
	require.Eventually(t, func() bool {
		status, body := f.request(t, http.MethodGet, "/control/model/state"+modelControlScope, "", f.key, true)
		if status != http.StatusOK {
			return false
		}
		require.NoError(t, json.Unmarshal([]byte(body), &value))
		return value.Phase == phase
	}, time.Second, 10*time.Millisecond)
	return value
}

func TestModelControlAfterConsumptionIsOneShotAndDoesNotOwnOtherCases(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t)
	f.install(t, "alice", 9, modelHoldAfter)
	f.install(t, "bob", 10, "")
	remote := FixtureRemote{URL: f.data.URL + "/lab/model"}
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
	result := make(chan error, 1)
	go func() { _, err := remote.Plan(ctx, agent.Input{Text: "private marker"}); result <- err }()
	state := f.state(t, "held_process_local")
	assert.True(t, state.Consumed)
	assert.False(t, state.Durable, "nil DB cannot certify a durable barrier")
	assert.Len(t, state.RequestSHA256, 64)
	assert.Len(t, state.ResponseSHA256, 64)
	select {
	case err := <-result:
		t.Fatalf("returned before release: %v", err)
	default:
	}
	_, err := remote.Plan(ctx, agent.Input{Text: "private marker"})
	require.Error(t, err, "a concurrent request cannot take the selected turn")
	plan, err := remote.Plan(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "bob", UpdateID: 10}),
		agent.Input{Text: "private marker"},
	)
	require.NoError(t, err)
	assert.Equal(t, "Synthetic answer", plan.Text)
	status, body := f.request(
		t,
		http.MethodPost,
		"/control/model/release"+modelControlScope,
		`{"action":"deliver"}`,
		f.key,
		true,
	)
	require.Equal(t, http.StatusOK, status, body)
	assert.NotContains(t, body, "private marker")
	assert.NotContains(t, body, "Synthetic answer")
	require.NoError(t, <-result)
	f.state(t, "response_generated")
	status, _ = f.request(
		t,
		http.MethodPost,
		"/control/model/release"+modelControlScope,
		`{"action":"deliver"}`,
		f.key,
		true,
	)
	assert.Equal(t, http.StatusConflict, status)
	f.fake.modelFixtures.mu.Lock()
	assert.Equal(t, 1, f.fake.modelFixtures.cases[modelFixtureKey("alice", 9)].accepted)
	f.fake.modelFixtures.mu.Unlock()
}

func TestModelControlBeforeConsumptionCancellationRetainsUnconsumedStep(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t)
	f.install(t, "alice", 9, modelHoldBefore)
	remote := FixtureRemote{URL: f.data.URL + "/lab/model"}
	ctx, cancel := context.WithCancel(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
	)
	result := make(chan error, 1)
	go func() { _, err := remote.Plan(ctx, agent.Input{Text: "private marker"}); result <- err }()
	state := f.state(t, "held_before_consume")
	assert.False(t, state.Consumed)
	_, duplicateErr := remote.Plan(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
		agent.Input{Text: "private marker"},
	)
	require.Error(t, duplicateErr, "a second matched request cannot bypass a before-consumption hold")
	assert.False(t, f.state(t, "held_before_consume").Consumed)
	cancel()
	require.Error(t, <-result)
	f.state(t, "request_cancelled")
	plan, err := remote.Plan(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
		agent.Input{Text: "private marker"},
	)
	require.NoError(t, err)
	assert.Equal(t, "Synthetic answer", plan.Text)
}

func TestModelControlFailuresNeverReinsertConsumedResponse(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"fail", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := newModelControlFixture(t)
			f.install(t, "alice", 9, modelHoldAfter)
			remote := FixtureRemote{URL: f.data.URL + "/lab/model"}
			ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
			result := make(chan error, 1)
			go func() { _, err := remote.Plan(ctx, agent.Input{Text: "private marker"}); result <- err }()
			f.state(t, "held_process_local")
			status, _ := f.request(
				t,
				http.MethodPost,
				"/control/model/release"+modelControlScope,
				`{"action":"`+action+`"}`,
				f.key,
				true,
			)
			require.Equal(t, http.StatusOK, status)
			require.Error(t, <-result)
			_, err := remote.Plan(ctx, agent.Input{Text: "private marker"})
			require.Error(t, err)
			f.fake.mu.Lock()
			snapshot := f.fake.modelConsumptionSnapshot()
			raw, err := json.Marshal(snapshot)
			f.fake.mu.Unlock()
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "private marker")
			assert.NotContains(t, string(raw), "Synthetic answer")
			reopened, err := New(t.Context(), nil, "synthetic-token")
			require.NoError(t, err)
			require.NoError(t, reopened.restoreModelConsumption(snapshot))
			state, exists := reopened.modelControl.view(modelFixtureScope{Owner: "alice", UpdateID: 9})
			require.True(t, exists)
			assert.Equal(t, "consumed_unavailable", state.Phase)
			require.Error(
				t,
				reopened.installModelCase(
					modelFixtureInstall{
						Owner:    "alice",
						UpdateID: 9,
						Steps:    []modelFixtureStep{fixtureStep("private marker")},
					},
				),
			)
		})
	}
}

func TestModelControlAuthorizationMatchingAndPersistencePending(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t)
	value := modelFixtureInstall{
		Owner:    "alice",
		UpdateID: 9,
		Steps:    []modelFixtureStep{fixtureStep("private marker")},
		Hold:     &modelFixtureHoldSetup{Mode: modelHoldAfter},
	}
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	status, _ := f.request(t, http.MethodPost, "/lab/model/fixtures", string(raw), "", false)
	assert.Equal(t, http.StatusForbidden, status)
	f.install(t, "alice", 9, modelHoldAfter)
	status, _ = f.request(t, http.MethodGet, "/control/model/state"+modelControlScope, "", "wrong", true)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = f.request(
		t,
		http.MethodPost,
		"/control/model/release"+modelControlScope,
		strings.Repeat("x", delayControlBodyLimit+1),
		f.key,
		true,
	)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
	remote := FixtureRemote{URL: f.data.URL + "/lab/model"}
	_, err = remote.Plan(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
		agent.Input{Text: "wrong"},
	)
	require.Error(t, err)
	f.state(t, "armed")
	row := modelConsumption{
		Owner:          "alice",
		UpdateID:       9,
		RequestSHA256:  strings.Repeat("a", 64),
		ResponseSHA256: strings.Repeat("b", 64),
	}
	f.fake.modelControl.consumptionStarted(row)
	status, _ = f.request(
		t,
		http.MethodPost,
		"/control/model/release"+modelControlScope,
		`{"action":"deliver"}`,
		f.key,
		true,
	)
	assert.Equal(t, http.StatusConflict, status, "no release before successful persistence")
	state := f.state(t, "persistence_pending")
	assert.True(t, state.Consumed)
	assert.False(t, state.Durable)
	for _, snapshot := range []*modelConsumptionSnapshot{
		{Version: 2, Rows: []modelConsumption{row}},
		{Version: 1, Rows: []modelConsumption{row, row}},
		{Version: 1, Rows: []modelConsumption{{Owner: "alice", UpdateID: 9, RequestSHA256: "invalid", ResponseSHA256: row.ResponseSHA256}}},
	} {
		require.Error(t, f.fake.restoreModelConsumption(snapshot))
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/lab/model/fixtures", bytes.NewReader(raw))
	request.Header.Set("X-R104-Control", f.key)
	response := httptest.NewRecorder()
	f.fake.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code, "control key does not replace sandbox admission")
}

// Environment and the reserved listener are process-wide; this case is serial.
func TestModelControlNativeStartupRequiresFreshExclusiveJournal(t *testing.T) {
	key := strings.Repeat("s", delayMinimumKeyBytes)
	firstJournal := filepath.Join(t.TempDir(), "generation-a.jsonl")
	t.Setenv("R104_CONTROL_KEY", key)
	t.Setenv("R104_JOURNAL", firstJournal)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	f, err := New(ctx, nil, "synthetic-token")
	require.NoError(t, err)
	data := httptest.NewServer(f.Handler())
	t.Cleanup(data.Close)
	fixture := modelControlFixture{
		fake:    f,
		data:    data,
		control: &httptest.Server{URL: "http://127.0.0.1:8090"},
		key:     key,
	}
	fixture.install(t, "alice", 9, modelHoldAfter)
	result := make(chan error, 1)
	go func() {
		_, planErr := (FixtureRemote{URL: data.URL + "/lab/model"}).Plan(
			agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
			agent.Input{Text: "private marker"},
		)
		result <- planErr
	}()
	fixture.state(t, "held_process_local")
	cancel()
	require.Error(t, <-result)
	select {
	case <-f.delay.journal.ended:
	case <-time.After(time.Second):
		t.Fatal("journal did not stop")
	}
	require.Eventually(t, func() bool {
		connection, dialErr := net.DialTimeout("tcp", "127.0.0.1:8090", 50*time.Millisecond)
		if dialErr != nil {
			return true
		}
		_ = connection.Close()
		return false
	}, time.Second, 10*time.Millisecond)
	old, err := os.ReadFile(firstJournal)
	require.NoError(t, err)
	oldSHA := sha256.Sum256(old)
	_, err = New(t.Context(), nil, "synthetic-token")
	require.Error(t, err, "an existing journal must never be appended or truncated")
	secondJournal := filepath.Join(filepath.Dir(firstJournal), "generation-b.jsonl")
	t.Setenv("R104_JOURNAL", secondJournal)
	ctx2, cancel2 := context.WithCancel(t.Context())
	t.Cleanup(cancel2)
	reopened, err := New(ctx2, nil, "synthetic-token")
	require.NoError(t, err)
	status, _ := fixture.request(t, http.MethodGet, "/control/model/state"+modelControlScope, "", key, true)
	assert.Equal(t, http.StatusNotFound, status, "nil DB has no durable consumption to reopen")
	cancel2()
	select {
	case <-reopened.delay.journal.ended:
	case <-time.After(time.Second):
		t.Fatal("second journal did not stop")
	}
	after, err := os.ReadFile(firstJournal)
	require.NoError(t, err)
	assert.Equal(t, oldSHA, sha256.Sum256(after))
	_, err = os.Stat(secondJournal)
	require.NoError(t, err)
}
