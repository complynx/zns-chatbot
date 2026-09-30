package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func fixtureAssessment(t *testing.T, worthwhile bool) (json.RawMessage, agent.KnowledgeAssessmentInput) {
	t.Helper()
	input := agent.KnowledgeAssessmentInput{
		Event:   "synthetic",
		Topic:   "travel",
		FactKey: "arrival",
		Text:    "Synthetic private fact.",
	}
	raw, err := json.Marshal(map[string]any{
		"expect": input,
		"result": agent.KnowledgeAssessment{Worthwhile: worthwhile, Reason: "Synthetic private reason."},
	})
	require.NoError(t, err)
	return raw, input
}

func TestFixtureAssessmentRemoteScopeConsumptionAndPlanIndependence(t *testing.T) {
	t.Parallel()
	for _, worthwhile := range []bool{false, true} {
		t.Run(strconv.FormatBool(worthwhile), func(t *testing.T) {
			t.Parallel()
			fake, err := New(t.Context(), nil, "synthetic-token")
			require.NoError(t, err)
			raw, input := fixtureAssessment(t, worthwhile)
			require.NoError(t, fake.modelFixtures.install(modelFixtureInstall{
				Owner: "alice", UpdateID: 9, Steps: []modelFixtureStep{fixtureStep("hello")}, Assessment: raw,
			}))
			server := httptest.NewServer(fake.Handler())
			t.Cleanup(server.Close)
			remote := FixtureRemote{URL: server.URL + "/lab/model"}
			for _, scope := range []agent.RequestScope{{Owner: "bob", UpdateID: 9}, {Owner: "alice", UpdateID: 10}} {
				_, err = remote.AssessKnowledge(agent.WithRequestScope(t.Context(), scope), input)
				require.Error(t, err)
			}
			ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
			for _, changed := range []agent.KnowledgeAssessmentInput{
				{Event: "other", Topic: input.Topic, FactKey: input.FactKey, Text: input.Text},
				{Event: input.Event, Topic: "other", FactKey: input.FactKey, Text: input.Text},
				{Event: input.Event, Topic: input.Topic, FactKey: "other", Text: input.Text},
				{Event: input.Event, Topic: input.Topic, FactKey: input.FactKey, Text: "other"},
			} {
				_, err = remote.AssessKnowledge(ctx, changed)
				require.Error(t, err)
			}
			verdict, err := remote.AssessKnowledge(ctx, input)
			require.NoError(t, err)
			assert.Equal(t, worthwhile, verdict.Worthwhile)
			assert.Equal(t, "Synthetic private reason.", verdict.Reason)
			_, err = remote.AssessKnowledge(ctx, input)
			require.Error(t, err)
			input.Text = "changed after consumption"
			_, err = remote.AssessKnowledge(ctx, input)
			require.Error(t, err)
			plan, err := remote.Plan(ctx, agent.Input{Text: "hello"})
			require.NoError(t, err)
			assert.Equal(t, "Synthetic answer", plan.Text)
			state := modelFixtureRequest(
				t,
				fake.Handler(),
				http.MethodGet,
				"/lab/model/state?owner=alice&update_id=9",
				nil,
			)
			var stats modelFixtureState
			require.NoError(t, json.Unmarshal(state.Body.Bytes(), &stats))
			assert.Equal(t, 1, stats.Accepted)
			assert.Zero(t, stats.Rejected)
			assert.Equal(
				t,
				modelFixtureAssessmentState{Configured: true, Accepted: 1, Rejected: 6, LastStatus: "input_mismatch"},
				stats.Assessment,
			)
			assert.NotContains(t, state.Body.String(), "Synthetic private")
		})
	}
}

func TestFixtureAssessmentBeforeProviderAndCancellationHaveNoWire(t *testing.T) {
	t.Parallel()
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	raw, input := fixtureAssessment(t, true)
	require.NoError(t, fake.modelFixtures.install(modelFixtureInstall{Owner: "alice", UpdateID: 9, Assessment: raw}))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fake.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	remote := FixtureRemote{URL: server.URL + "/lab/model"}
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
	denied := errors.New("synthetic authority denied")
	input.BeforeProvider = func(context.Context) error { return denied }
	_, err = remote.AssessKnowledge(ctx, input)
	require.ErrorIs(t, err, denied)
	input.BeforeProvider = nil
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = remote.AssessKnowledge(cancelled, input)
	require.ErrorIs(t, err, context.Canceled)
	_, err = remote.AssessKnowledge(t.Context(), input)
	require.Error(t, err)
	_, err = remote.AssessKnowledge(
		agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "real-person", UpdateID: 9}),
		input,
	)
	require.Error(t, err)
	assert.Zero(t, calls.Load())
	_, err = remote.AssessKnowledge(ctx, input)
	require.NoError(t, err, "denial did not consume the configured assessment")
	assert.Equal(t, int32(1), calls.Load())
}

func TestFixtureAssessmentConcurrentConsumption(t *testing.T) {
	t.Parallel()
	var store modelFixtures
	raw, input := fixtureAssessment(t, true)
	require.NoError(t, store.install(modelFixtureInstall{Owner: "alice", UpdateID: 9, Assessment: raw}))
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if _, err := store.assess(modelFixtureScope{Owner: "alice", UpdateID: 9}, input); err == nil {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	assert.Equal(t, int32(1), accepted.Load())
}

func TestFixtureAssessmentStrictInstallAndSharedCapacity(t *testing.T) {
	t.Parallel()
	raw, _ := fixtureAssessment(t, true)
	for name, invalid := range map[string]string{
		"null":            "null",
		"missing result":  `{"expect":{"event":"","topic":"","fact_key":"","text":"fact"}}`,
		"missing verdict": strings.Replace(string(raw), `"worthwhile":true`, `"other":true`, 1),
		"null verdict":    strings.Replace(string(raw), `"worthwhile":true`, `"worthwhile":null`, 1),
		"missing reason":  strings.Replace(string(raw), `,"reason":"Synthetic private reason."`, "", 1),
		"null reason":     strings.Replace(string(raw), `"reason":"Synthetic private reason."`, `"reason":null`, 1),
		"missing input":   strings.Replace(string(raw), `"event":"synthetic",`, "", 1),
		"duplicate":       strings.Replace(string(raw), `"event":"synthetic"`, `"event":"synthetic","event":"other"`, 1),
		"long reason":     strings.Replace(string(raw), "Synthetic private reason.", strings.Repeat("x", 513), 1),
		"long text":       strings.Replace(string(raw), "Synthetic private fact.", strings.Repeat("x", 2001), 1),
		"long identifier": strings.Replace(string(raw), "synthetic", strings.Repeat("x", 101), 1),
		"invalid utf8":    string([]byte{0xff}),
		"nul reason":      strings.Replace(string(raw), "Synthetic private reason.", `\u0000`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, err := New(t.Context(), nil, "synthetic-token")
			require.NoError(t, err)
			_, err = fake.installAndEnqueueFixture(t.Context(), modelFixtureInstall{
				Input: &modelFixtureInput{User: 101, Text: "synthetic"}, Assessment: json.RawMessage(invalid),
			})
			require.Error(t, err)
			assert.Empty(t, fake.updates)
			assert.Empty(t, fake.modelFixtures.cases)
		})
	}
	var store modelFixtures
	steps := make([]modelFixtureStep, maxModelFixtureSteps)
	for i := range steps {
		steps[i] = fixtureStep("hello")
	}
	require.Error(t, store.install(modelFixtureInstall{Owner: "alice", UpdateID: 1, Steps: steps, Assessment: raw}))
	require.NoError(
		t,
		store.install(modelFixtureInstall{Owner: "alice", UpdateID: 1, Steps: steps[:31], Assessment: raw}),
	)
	require.Error(t, store.install(modelFixtureInstall{Owner: "bob", UpdateID: 2, Assessment: raw}))
}

func TestFixtureAssessmentAtomicInputAndMissingConfiguration(t *testing.T) {
	t.Parallel()
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	raw, input := fixtureAssessment(t, true)
	update, err := fake.installAndEnqueueFixture(t.Context(), modelFixtureInstall{
		Input: &modelFixtureInput{
			User: 101,
			Text: "hello",
		},
		Steps:      []modelFixtureStep{fixtureStep("hello")},
		Assessment: raw,
	})
	require.NoError(t, err)
	require.Len(t, fake.updates, 1)
	assert.Equal(t, update, fake.updates[0].ID)
	verdict, err := fake.modelFixtures.assess(modelFixtureScope{Owner: "alice", UpdateID: update}, input)
	require.NoError(t, err, "published update already has its assessment")
	assert.True(t, verdict.Worthwhile)
	_, err = fake.modelFixtures.plan(modelFixtureScope{Owner: "alice", UpdateID: update}, agent.Input{Text: "hello"})
	require.NoError(t, err, "assessment did not consume the plan")
	require.NoError(
		t,
		fake.modelFixtures.install(
			modelFixtureInstall{Owner: "bob", UpdateID: 900, Steps: []modelFixtureStep{fixtureStep("hello")}},
		),
	)
	_, err = fake.modelFixtures.assess(modelFixtureScope{Owner: "bob", UpdateID: 900}, input)
	require.Error(t, err, "a plan does not imply a default assessment")
}

func TestFixtureAssessmentLabFenceAndRedirect(t *testing.T) {
	t.Parallel()
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	raw, input := fixtureAssessment(t, true)
	require.NoError(t, fake.modelFixtures.install(modelFixtureInstall{Owner: "alice", UpdateID: 9, Assessment: raw}))
	body, err := json.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/lab/model/knowledge-assessment",
		bytes.NewReader(body),
	)
	response := httptest.NewRecorder()
	fake.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code)
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 9})
	_, err = (FixtureRemote{URL: origin.URL}).AssessKnowledge(ctx, input)
	require.Error(t, err)
	assert.Zero(t, redirected.Load())
}

func TestFixtureAssessmentRejectsIncompleteWireInputWithoutConsumption(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"event", "topic", "fact_key", "text"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			fake, err := New(t.Context(), nil, "synthetic-token")
			require.NoError(t, err)
			raw, input := fixtureAssessment(t, true)
			require.NoError(
				t,
				fake.modelFixtures.install(modelFixtureInstall{Owner: "alice", UpdateID: 9, Assessment: raw}),
			)
			fields := map[string]string{
				"event":    input.Event,
				"topic":    input.Topic,
				"fact_key": input.FactKey,
				"text":     input.Text,
			}
			delete(fields, missing)
			body, encodeErr := json.Marshal(fields)
			require.NoError(t, encodeErr)
			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				"/lab/model/knowledge-assessment",
				bytes.NewReader(body),
			)
			request.Header.Set("X-Sandbox", "1")
			request.Header.Set("X-Sandbox-Actor", "alice")
			request.Header.Set("X-Sandbox-Update", "9")
			response := httptest.NewRecorder()
			fake.Handler().ServeHTTP(response, request)
			assert.Equal(t, http.StatusBadRequest, response.Code)
			_, err = fake.modelFixtures.assess(modelFixtureScope{Owner: "alice", UpdateID: 9}, input)
			require.NoError(t, err)
		})
	}
}

func TestFixtureAssessmentRejectsCaseAliasesAtInstall(t *testing.T) {
	t.Parallel()
	raw, _ := fixtureAssessment(t, true)
	valid := `{"input":{"user":101,"text":"synthetic"},"assessment":` + string(raw) + `}`
	for name, body := range map[string]string{
		"assessment":     strings.Replace(valid, `"assessment":`, `"Assessment":`, 1),
		"expect":         strings.Replace(valid, `"expect":`, `"Expect":`, 1),
		"result":         strings.Replace(valid, `"result":`, `"Result":`, 1),
		"event":          strings.Replace(valid, `"event":`, `"Event":`, 1),
		"worthwhile":     strings.Replace(valid, `"worthwhile":`, `"Worthwhile":`, 1),
		"reason":         strings.Replace(valid, `"reason":`, `"Reason":`, 1),
		"override false": strings.Replace(valid, `"worthwhile":true`, `"worthwhile":true,"Worthwhile":false`, 1),
		"override true":  strings.Replace(valid, `"worthwhile":true`, `"Worthwhile":false,"worthwhile":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake, err := New(t.Context(), nil, "synthetic-token")
			require.NoError(t, err)
			response := modelFixtureRequest(t, fake.Handler(), http.MethodPost, "/lab/model/fixtures", []byte(body))
			assert.Contains(t, []int{http.StatusBadRequest, http.StatusConflict}, response.Code)
			assert.Empty(t, fake.updates, "invalid assessment must not publish its input")
			assert.Empty(t, fake.modelFixtures.cases)
		})
	}
}

func TestFixtureAssessmentRejectsCaseAliasesAtRequest(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"event", "topic", "fact_key", "text"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			fake, err := New(t.Context(), nil, "synthetic-token")
			require.NoError(t, err)
			raw, input := fixtureAssessment(t, true)
			require.NoError(
				t,
				fake.modelFixtures.install(modelFixtureInstall{Owner: "alice", UpdateID: 9, Assessment: raw}),
			)
			data, err := json.Marshal(input)
			require.NoError(t, err)
			for _, body := range []string{
				strings.Replace(string(data), `"`+field+`":`, `"`+strings.ToUpper(field)+`":`, 1),
				strings.Replace(string(data), `"`+field+`":`, `"`+strings.ToUpper(field)+`":"wrong","`+field+`":`, 1),
			} {
				request := httptest.NewRequestWithContext(
					t.Context(),
					http.MethodPost,
					"/lab/model/knowledge-assessment",
					strings.NewReader(body),
				)
				request.Header.Set("X-Sandbox", "1")
				request.Header.Set("X-Sandbox-Actor", "alice")
				request.Header.Set("X-Sandbox-Update", "9")
				response := httptest.NewRecorder()
				fake.Handler().ServeHTTP(response, request)
				assert.Equal(t, http.StatusBadRequest, response.Code)
			}
			_, err = fake.modelFixtures.assess(modelFixtureScope{Owner: "alice", UpdateID: 9}, input)
			require.NoError(t, err, "case aliases must not consume the fixture")
		})
	}
}

func TestFixtureAssessmentPreservesExistingPlanFieldDecoding(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(
		modelFixtureInstall{Owner: "alice", UpdateID: 9, Steps: []modelFixtureStep{fixtureStep("hello")}},
	)
	require.NoError(t, err)
	body = bytes.Replace(body, []byte(`"owner":`), []byte(`"Owner":`), 1)
	var value modelFixtureInstall
	require.NoError(t, strictFixtureJSON(body, &value))
	var store modelFixtures
	require.NoError(t, store.install(value))
	_, err = store.plan(modelFixtureScope{Owner: "alice", UpdateID: 9}, agent.Input{Text: "hello"})
	require.NoError(t, err)
}
