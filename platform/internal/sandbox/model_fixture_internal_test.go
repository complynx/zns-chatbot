package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func fixtureStep(text string) modelFixtureStep {
	expect, _ := json.Marshal(map[string]string{"text": text})
	return modelFixtureStep{Expect: expect, Plan: agent.Plan{View: "workflow", Text: "Synthetic answer"}}
}

func TestModelFixtureSequenceOwnershipAndSanitizedState(t *testing.T) {
	t.Parallel()
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	handler := fake.Handler()
	first := fixtureStep("private synthetic marker")
	first.Plan = agent.Plan{
		View:         "workflow",
		ScriptAction: &agent.ScriptProposal{Code: "return input.n+1", InputJSON: `{"n":1}`},
	}
	second := fixtureStep("private synthetic marker")
	second.Expect = json.RawMessage(`{"text":"private synthetic marker","script":{"runs":[{"result":2}]}}`)
	value := modelFixtureInstall{Owner: "alice", UpdateID: 9, Steps: []modelFixtureStep{first, second}}
	body, err := json.Marshal(value)
	require.NoError(t, err)
	response := modelFixtureRequest(t, handler, http.MethodPost, "/lab/model/fixtures", body)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	input := agent.Input{
		Text:   "private synthetic marker",
		Script: &agent.ScriptContext{Available: true, Remaining: 2, Runs: []agent.ScriptRun{}},
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	remote := FixtureRemote{URL: server.URL + "/lab/model"}
	_, err = remote.Plan(t.Context(), input)
	require.Error(t, err, "no host scope must fail before I/O")
	_, err = remote.Plan(agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "bob", UpdateID: 9}), input)
	require.Error(t, err, "other owner cannot consume Alice's queue")
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
	plan, err := remote.Plan(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, plan.ScriptAction)
	_, err = remote.Plan(ctx, input)
	require.Error(t, err, "same turn cannot consume another step")
	ctx = agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9, Turn: 1})
	_, err = remote.Plan(ctx, input)
	require.Error(t, err, "tool result is required")
	input.Script.Runs = []agent.ScriptRun{{Result: json.RawMessage(`2`)}}
	plan, err = remote.Plan(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, "Synthetic answer", plan.Text)
	state := modelFixtureRequest(
		t,
		handler,
		http.MethodGet,
		"/lab/model/state?owner=alice&update_id=9",
		nil,
	)
	require.Equal(t, http.StatusOK, state.Code)
	assert.NotContains(t, state.Body.String(), "private synthetic marker")
	assert.NotContains(t, state.Body.String(), "return input")
	var stats modelFixtureState
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &stats))
	assert.Equal(t, 2, stats.Accepted)
	assert.Equal(t, 2, stats.Rejected)
	other := modelFixtureRequest(t, handler, http.MethodGet, "/lab/model/state?owner=bob&update_id=9", nil)
	assert.Equal(t, http.StatusNotFound, other.Code)
}

func modelFixtureRequest(
	t *testing.T,
	handler http.Handler,
	method, path string,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(body))
	request.Header.Set("X-Sandbox", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestModelFixtureRejectsMalformedAndBoundedQueues(t *testing.T) {
	t.Parallel()
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	for _, body := range []string{
		`{"owner":"alice","owner":"bob","update_id":1,"steps":[]}`,
		`{"owner":"alice","update_id":1,"unexpected":true}`,
		`{"owner":"alice","update_id":1,"steps":[{"expect":{"text":"x","unknown":1},"plan":{"view":"workflow"}}]}`,
		`{"owner":"alice","update_id":1,"steps":[{"expect":{"text":"x"},"plan":{"view":"workflow","action":{"name":"confirm"}}}]}`,
		strings.Repeat("x", maxModelFixtureBytes+1),
	} {
		response := modelFixtureRequest(
			t,
			fake.Handler(),
			http.MethodPost,
			"/lab/model/fixtures",
			[]byte(body),
		)
		assert.GreaterOrEqual(t, response.Code, http.StatusBadRequest)
	}
	engine := modelFixtures{}
	for index := range maxModelFixtureSteps {
		require.NoError(
			t,
			engine.install(
				modelFixtureInstall{
					Owner:    "alice",
					UpdateID: int64(index + 1),
					Steps:    []modelFixtureStep{fixtureStep("x")},
				},
			),
		)
	}
	require.Error(
		t,
		engine.install(modelFixtureInstall{Owner: "alice", UpdateID: 100, Steps: []modelFixtureStep{fixtureStep("x")}}),
	)
	large := modelFixtures{}
	for index := range 4 {
		require.NoError(
			t,
			large.install(
				modelFixtureInstall{
					Owner:    "alice",
					UpdateID: int64(index + 1),
					Steps:    []modelFixtureStep{fixtureStep(strings.Repeat("x", 60000))},
				},
			),
		)
	}
	require.Error(
		t,
		large.install(
			modelFixtureInstall{
				Owner:    "alice",
				UpdateID: 5,
				Steps:    []modelFixtureStep{fixtureStep(strings.Repeat("x", 60000))},
			},
		),
	)
	require.Error(
		t,
		large.install(
			modelFixtureInstall{
				Owner:    "bob",
				UpdateID: 6,
				Steps:    []modelFixtureStep{fixtureStep(strings.Repeat("x", maxModelFixtureStepBytes))},
			},
		),
	)
}

func TestModelFixtureParallelConsumptionIsAtomic(t *testing.T) {
	t.Parallel()
	engine := modelFixtures{}
	require.NoError(
		t,
		engine.install(
			modelFixtureInstall{
				Owner:    "alice",
				UpdateID: 1,
				Steps:    []modelFixtureStep{fixtureStep("x"), fixtureStep("x")},
			},
		),
	)
	var accepted atomic.Int32
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			_, err := engine.plan(modelFixtureScope{Owner: "alice", UpdateID: 1}, agent.Input{Text: "x"})
			if err == nil {
				accepted.Add(1)
			}
		})
	}
	group.Wait()
	assert.EqualValues(t, 1, accepted.Load())
	_, err := engine.plan(modelFixtureScope{Owner: "alice", UpdateID: 1, Turn: 1}, agent.Input{Text: "x"})
	require.NoError(t, err)
	_, err = engine.plan(modelFixtureScope{Owner: "alice", UpdateID: 1, Turn: 2}, agent.Input{Text: "x"})
	require.Error(t, err, "exhausted cases never fall back")
}

func TestModelFixtureAtomicInput(t *testing.T) {
	t.Parallel()
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	value := modelFixtureInstall{
		Input: &modelFixtureInput{User: 101, Text: "compute"},
		Steps: []modelFixtureStep{fixtureStep("compute")},
	}
	update, err := fake.installAndEnqueueFixture(t.Context(), value)
	require.NoError(t, err)
	require.Len(t, fake.updates, 1)
	assert.Equal(t, update, fake.updates[0].ID)
	assert.Equal(t, "compute", fake.updates[0].Message.Text)
	_, err = fake.modelFixtures.plan(modelFixtureScope{Owner: "alice", UpdateID: update}, agent.Input{Text: "compute"})
	require.NoError(t, err)
	value.Owner = "bob"
	_, err = fake.installAndEnqueueFixture(t.Context(), value)
	require.Error(t, err)
	assert.Len(t, fake.updates, 1, "invalid scope must not enqueue")
	value.Owner = ""
	value.Steps = nil
	_, err = fake.installAndEnqueueFixture(t.Context(), value)
	require.Error(t, err)
	assert.Len(t, fake.updates, 1, "invalid plan must not enqueue")
}

func TestModelFixtureRemoteRejectsRedirectAndCancellation(t *testing.T) {
	t.Parallel()
	var reached atomic.Int32
	destination := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached.Add(1); w.WriteHeader(http.StatusOK) }),
	)
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	model := FixtureRemote{URL: redirect.URL}
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 1})
	_, err := model.Plan(ctx, agent.Input{Text: "x"})
	require.Error(t, err)
	assert.Zero(t, reached.Load())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = model.Plan(canceled, agent.Input{Text: "x"})
	require.Error(t, err)
	assert.Zero(t, reached.Load())
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/lab/model/fixtures", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	fake.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code)
}
