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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type modelControlFixture struct {
	fake    *Fake
	data    *httptest.Server
	control *httptest.Server
	key     string
}

func newModelControlFixture(t *testing.T) modelControlFixture {
	return newModelControlFixtureContext(t.Context(), t)
}

func newModelControlFixtureContext(ctx context.Context, t *testing.T) modelControlFixture {
	t.Helper()
	f, err := New(ctx, nil, "synthetic-token")
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

type blockedModelStateWriter struct {
	header  http.Header
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedModelStateWriter) Header() http.Header { return w.header }
func (w *blockedModelStateWriter) WriteHeader(_ int) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
}
func (w *blockedModelStateWriter) Write(value []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return len(value), nil
}

func TestModelStateBlockedHTTPWriterDoesNotOwnFixtureLock(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/lab/model/state?owner=alice&update_id=9", "/lab/model/state?owner=alice&update_id=99"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := newModelControlFixture(t)
			f.install(t, "alice", 9, "")
			f.install(t, "bob", 10, "")
			writer := &blockedModelStateWriter{
				header:  make(http.Header),
				entered: make(chan struct{}),
				release: make(chan struct{}),
			}
			done := make(chan struct{})
			t.Cleanup(func() {
				close(writer.release)
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("state writer did not terminate")
				}
			})
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			request.Header.Set("X-Sandbox", "1")
			go func() { f.fake.Handler().ServeHTTP(writer, request); close(done) }()
			select {
			case <-writer.entered:
			case <-time.After(time.Second):
				t.Fatal("state writer did not start")
			}
			ctx, cancel := context.WithTimeout(
				agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "bob", UpdateID: 10}),
				time.Second,
			)
			defer cancel()
			plan, err := (FixtureRemote{URL: f.data.URL + "/lab/model"}).Plan(ctx, agent.Input{Text: "private marker"})
			require.NoError(t, err, "a blocked success/error response cannot block unrelated selection")
			assert.Equal(t, "Synthetic answer", plan.Text)
		})
	}
}

func TestModelSelectionAndConsumptionBoundNestedMutexAdmission(t *testing.T) {
	t.Parallel()
	for _, consume := range []bool{false, true} {
		t.Run(map[bool]string{false: "selection", true: "consumption"}[consume], func(t *testing.T) {
			t.Parallel()
			f := newModelControlFixture(t)
			f.install(t, "alice", 9, "")
			f.fake.modelFixtures.mu.Lock()
			defer f.fake.modelFixtures.mu.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				scope := modelFixtureScope{Owner: "alice", UpdateID: 9}
				if consume {
					_, err := f.fake.consumeModelPlan(
						ctx,
						scope,
						agent.Input{Text: "private marker"},
						modelFixtureSelection{},
					)
					done <- err
				} else {
					_, err := f.fake.selectModelHold(ctx, scope, agent.Input{Text: "private marker"})
					done <- err
				}
			}()
			select {
			case err := <-done:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("nested mutex ignored cancellation")
			}
			require.True(
				t,
				f.fake.mu.TryLock(),
				"mutation lock must be released before the nested mutex becomes available",
			)
			f.fake.mu.Unlock()
		})
	}
}

func TestModelReleaseReadyAndCancellationReadyCannotWinDelivery(t *testing.T) {
	t.Parallel()
	for _, providerStop := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "provider"}[providerStop], func(t *testing.T) {
			t.Parallel()
			for index := range 64 {
				mode := modelHoldBefore
				if index%2 == 0 {
					mode = modelHoldAfter
				}
				lifetime, stop := context.WithCancel(t.Context())
				request, cancel := context.WithCancel(t.Context())
				c := newModelFixtureControl(lifetime)
				scope := modelFixtureScope{Owner: "alice", UpdateID: 9}
				c.install(
					modelFixtureInstall{
						Owner:    "alice",
						UpdateID: 9,
						Hold:     &modelFixtureHoldSetup{Mode: mode},
					},
				)
				hold, err := c.claim(scope)
				require.NoError(t, err)
				if mode == modelHoldAfter {
					c.consumed(modelConsumption{Owner: "alice", UpdateID: 9}, false)
				}
				_, status := c.release(scope, "deliver")
				require.Equal(t, http.StatusOK, status)
				if providerStop {
					stop()
				} else {
					cancel()
				}
				require.Error(t, c.wait(request, scope, hold), "both ready: cancellation must precede release success")
				state, exists := c.view(scope)
				require.True(t, exists)
				assert.Equal(t, mode == modelHoldAfter, state.Consumed)
				assert.Equal(
					t,
					map[bool]string{false: "request_cancelled", true: "provider_stopped"}[providerStop],
					state.Phase,
				)
				stop()
				cancel()
			}
		})
	}
}

func TestModelProviderStopRejectsReleaseInActualDrainingHTTP(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{modelHoldBefore, modelHoldAfter} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			lifetime, stop := context.WithCancel(t.Context())
			defer stop()
			f := newModelControlFixtureContext(lifetime, t)
			f.install(t, "alice", 9, mode)
			result := make(chan error, 1)
			go func() {
				_, err := (FixtureRemote{URL: f.data.URL + "/lab/model"}).Plan(
					agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
					agent.Input{Text: "private marker"},
				)
				result <- err
			}()
			phase := "held_before_consume"
			if mode == modelHoldAfter {
				phase = "held_process_local"
			}
			f.state(t, phase)
			stop()
			status, _ := f.request(
				t,
				http.MethodPost,
				"/control/model/release"+modelControlScope,
				`{"action":"deliver"}`,
				f.key,
				true,
			)
			assert.Equal(t, http.StatusConflict, status)
			select {
			case err := <-result:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("provider stop did not end draining request")
			}
			state := f.state(t, "provider_stopped")
			assert.Equal(t, mode == modelHoldAfter, state.Consumed)
			f.fake.modelFixtures.mu.Lock()
			assert.Equal(
				t,
				map[bool]int{false: 0, true: 1}[mode == modelHoldAfter],
				f.fake.modelFixtures.cases[modelFixtureKey("alice", 9)].accepted,
			)
			f.fake.modelFixtures.mu.Unlock()
		})
	}
}

func TestModelProviderStopCancelsActualSelectionLockWait(t *testing.T) {
	t.Parallel()
	lifetime, stop := context.WithCancel(t.Context())
	defer stop()
	f := newModelControlFixtureContext(lifetime, t)
	f.install(t, "alice", 9, modelHoldBefore)
	f.fake.modelFixtures.mu.Lock()
	defer f.fake.modelFixtures.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		_, err := (FixtureRemote{URL: f.data.URL + "/lab/model"}).Plan(
			agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9}),
			agent.Input{Text: "private marker"},
		)
		result <- err
	}()
	require.Eventually(t, func() bool {
		if f.fake.mu.TryLock() {
			f.fake.mu.Unlock()
			return false
		}
		return true
	}, time.Second, time.Millisecond)
	stop()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("provider stop did not cancel nested admission")
	}
	require.True(t, f.fake.mu.TryLock())
	f.fake.mu.Unlock()
	f.state(t, "armed")
	assert.Zero(t, f.fake.modelFixtures.cases[modelFixtureKey("alice", 9)].accepted)
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

func TestModelCancelledDuplicateCannotFinishClaimedRequest(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{modelHoldBefore, modelHoldAfter} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newModelControlFixture(t)
			f.install(t, "alice", 9, mode)
			f.install(t, "bob", 10, "")
			remote := FixtureRemote{URL: f.data.URL + "/lab/model"}
			ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
			result := make(chan error, 1)
			go func() { _, err := remote.Plan(ctx, agent.Input{Text: "private marker"}); result <- err }()
			phase := "held_before_consume"
			if mode == modelHoldAfter {
				phase = "held_process_local"
			}
			before := f.state(t, phase)
			duplicate, cancel := context.WithCancel(t.Context())
			cancel()
			_, err := f.fake.fixtureModelPlan(duplicate, modelFixtureScope{Owner: "alice", UpdateID: 9},
				agent.Input{Text: "private marker"})
			require.Error(t, err)
			testModelDuplicateAdmissionCancellation(t, f.fake)
			after, exists := f.fake.modelControl.view(modelFixtureScope{Owner: "alice", UpdateID: 9})
			require.True(t, exists)
			assert.Equal(t, before, after, "a nonowner must preserve the active claim and consumption evidence")
			_, err = remote.Plan(ctx, agent.Input{Text: "private marker"})
			require.Error(t, err, "the original claim must still exclude another request")
			_, err = remote.Plan(agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "bob", UpdateID: 10}),
				agent.Input{Text: "private marker"})
			require.NoError(t, err)
			status, _ := f.request(t, http.MethodPost, "/control/model/release"+modelControlScope,
				`{"action":"deliver"}`, f.key, true)
			require.Equal(t, http.StatusOK, status)
			select {
			case err = <-result:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("original request did not finish after release")
			}
			state := f.state(t, "response_generated")
			assert.True(t, state.Consumed)
			assert.False(t, state.Durable, "nil DB must remain process local")
		})
	}
}

func testModelDuplicateAdmissionCancellation(t *testing.T, f *Fake) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.fixtureModelPlan(ctx, modelFixtureScope{Owner: "alice", UpdateID: 9},
			agent.Input{Text: "private marker"})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("duplicate admission ended before cancellation: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancelled duplicate did not leave admission")
	}
}

func TestModelSelectionIdentityPreservesDurableClaimAndRejectsStaleCleanup(t *testing.T) {
	t.Parallel()
	c := newModelFixtureControl(t.Context())
	scope := modelFixtureScope{Owner: "alice", UpdateID: 9}
	c.install(modelFixtureInstall{Owner: scope.Owner, UpdateID: scope.UpdateID,
		Hold: &modelFixtureHoldSetup{Mode: modelHoldAfter}})
	first, err := c.claim(scope)
	require.NoError(t, err)
	c.consumed(modelConsumption{Owner: scope.Owner, UpdateID: scope.UpdateID,
		RequestSHA256: strings.Repeat("a", 64), ResponseSHA256: strings.Repeat("b", 64)}, true)
	before, exists := c.view(scope)
	require.True(t, exists)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.True(t, c.cancelled(ctx, scope, modelFixtureSelection{}))
	after, exists := c.view(scope)
	require.True(t, exists)
	assert.Equal(t, before, after)
	_, status := c.release(scope, "deliver")
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, c.wait(t.Context(), scope, first))
	c.finishSelection(scope, first, "response_generated")
	second, err := c.claim(scope)
	require.NoError(t, err)
	c.finishSelection(scope, first, "request_cancelled")
	_, err = c.claim(scope)
	require.Error(t, err, "stale cleanup cannot clear the new selection")
	c.finishSelection(scope, second, "unavailable")
	after, exists = c.view(scope)
	require.True(t, exists)
	assert.True(t, after.Durable)
	assert.True(t, after.Consumed)
}

func TestModelInstallAdmissionEndsBeforeBlockedMutation(t *testing.T) {
	t.Parallel()
	for _, nested := range []bool{false, true} {
		for _, enqueue := range []bool{false, true} {
			for _, ending := range []string{"request_cancelled", "provider_stopped", "deadline"} {
				t.Run(stringTurn(map[bool]int{false: 0, true: 1}[nested])+"/"+
					stringTurn(map[bool]int{false: 0, true: 1}[enqueue])+"/"+ending, func(t *testing.T) {
					t.Parallel()
					testModelInstallAdmission(t, nested, enqueue, ending)
				})
			}
		}
	}
}

func testModelInstallAdmission(t *testing.T, nested, enqueue bool, ending string) {
	t.Helper()
	lifetime, stop := context.WithCancel(t.Context())
	defer stop()
	f := newModelControlFixtureContext(lifetime, t)
	value := modelFixtureInstall{Owner: "alice", UpdateID: 9,
		Steps: []modelFixtureStep{fixtureStep("private marker")}, Hold: &modelFixtureHoldSetup{Mode: modelHoldBefore}}
	if enqueue {
		value.Owner, value.UpdateID = "", 0
		value.Input = &modelFixtureInput{User: identity.AliceTelegramID, Text: "private marker"}
	}
	mutex := &f.fake.mu
	if nested {
		mutex = &f.fake.modelFixtures.mu
	}
	mutex.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.fake.installAndEnqueueFixture(ctx, value); done <- err }()
	select {
	case err := <-done:
		mutex.Unlock()
		t.Fatalf("installation ended before cancellation: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	switch ending {
	case "request_cancelled":
		cancel()
	case "provider_stopped":
		stop()
	}
	select {
	case err := <-done:
		mutex.Unlock()
		require.Error(t, err)
	case <-time.After(time.Second):
		mutex.Unlock()
		t.Fatal("installation admission outlived cancellation while the mutex remained held")
	}
	assert.Empty(t, f.fake.modelFixtures.cases)
	assert.Empty(t, f.fake.modelControl.entries)
	assert.Empty(t, f.fake.messages)
	assert.Empty(t, f.fake.updates)
	assert.Zero(t, f.fake.next)
	assert.Empty(t, f.fake.modelConsumed)
	if ending != "provider_stopped" {
		f.install(t, "bob", 10, "")
	}
}

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
					t.Context(),
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
