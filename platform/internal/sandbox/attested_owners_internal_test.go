package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAttestedSandboxActorsReceiveRepliesAndLaunchOwnedMiniApp(t *testing.T) {
	t.Parallel()
	fake, err := NewWithAttestedOwners(t.Context(), nil, "synthetic-token", map[int64]string{
		101: "owner-101", 202: "owner-202", 303: "owner-303",
	})
	require.NoError(t, err)
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: fake.Token}
	address := "https://example.test/menu"
	for _, sender := range []int64{101, 202, 303} {
		message, sendErr := client.Send(
			t.Context(),
			telegram.Send{ChatID: sender, Text: "Menu", Markup: telegram.Markup{
				Rows: [][]telegram.Button{{{Text: "Menu", WebApp: &telegram.WebApp{URL: address}}}},
			}},
		)
		require.NoError(t, sendErr)
		require.Equal(t, sender, message.Chat.ID)
		payload, marshalErr := json.Marshal(map[string]any{"user": sender, "message_id": message.ID, "url": address})
		require.NoError(t, marshalErr)
		request := httptest.NewRequest(http.MethodPost, "/lab/webapp", strings.NewReader(string(payload)))
		request.Header.Set("X-Sandbox", "1")
		response := httptest.NewRecorder()
		fake.Handler().ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			URL string `json:"url"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		launched, parseErr := url.Parse(result.URL)
		require.NoError(t, parseErr)
		fragment, fragmentErr := url.ParseQuery(launched.EscapedFragment())
		require.NoError(t, fragmentErr)
		actor, verifyErr := telegram.VerifyWebApp(fragment.Get("tgWebAppData"), fake.Token, time.Now())
		require.NoError(t, verifyErr)
		require.Equal(t, sender, actor.ID)
	}
	_, err = client.Send(t.Context(), telegram.Send{ChatID: 404, Text: "Denied"})
	require.Error(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"/lab/webapp",
		strings.NewReader(`{"user":404,"message_id":1,"url":"https://example.test/menu"}`),
	)
	request.Header.Set("X-Sandbox", "1")
	response := httptest.NewRecorder()
	fake.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAttestedSandboxFixtureUsesMappedOwnerWithoutFallback(t *testing.T) {
	t.Parallel()
	owners := map[int64]string{101: "owner-101", 202: "owner-202", 303: "owner-303"}
	fake, err := NewWithAttestedOwners(t.Context(), nil, "synthetic-token", owners)
	require.NoError(t, err)
	owners[101] = "foreign-owner"
	update, err := fake.installAndEnqueueFixture(t.Context(), modelFixtureInstall{
		Input: &modelFixtureInput{User: 101, Text: "hello"},
		Steps: []modelFixtureStep{fixtureStep("hello")},
	})
	require.NoError(t, err)
	_, err = fake.modelFixtures.plan(modelFixtureScope{Owner: "alice", UpdateID: update}, agent.Input{Text: "hello"})
	require.Error(t, err)
	_, err = fake.modelFixtures.plan(
		modelFixtureScope{Owner: "owner-101", UpdateID: update},
		agent.Input{Text: "hello"},
	)
	require.NoError(t, err)
	_, err = fake.installAndEnqueueFixture(t.Context(), modelFixtureInstall{
		Input: &modelFixtureInput{User: 404, Text: "hello"},
		Steps: []modelFixtureStep{fixtureStep("hello")},
	})
	require.Error(t, err)
}

func TestAttestedSandboxPublicInputAndStateDenyUnmappedActor(t *testing.T) {
	t.Parallel()
	fake, err := NewWithAttestedOwners(t.Context(), nil, "synthetic-token", map[int64]string{101: "owner-101"})
	require.NoError(t, err)
	handler := fake.Handler()
	for _, test := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/lab/input", `{"user":101,"text":"hello"}`, http.StatusOK},
		{http.MethodGet, "/lab/state?user=101", "", http.StatusOK},
		{http.MethodPost, "/lab/input", `{"user":202,"text":"hello"}`, http.StatusBadRequest},
		{http.MethodGet, "/lab/state?user=202", "", http.StatusBadRequest},
		{http.MethodPost, "/lab/input", `{"user":404,"text":"hello"}`, http.StatusBadRequest},
		{http.MethodGet, "/lab/state?user=404", "", http.StatusBadRequest},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("X-Sandbox", "1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/lab/callback-receipts?user=101", nil)
	request.Header.Set("X-Sandbox", "1")
	request.Header.Set("X-Sandbox-Actor", "alice")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, "provider actor slots stay separate from domain owners")
}

func TestAttestedSandboxConstructorValidationAndDefaultOwners(t *testing.T) {
	t.Parallel()
	for _, owners := range []map[int64]string{{}, {0: "owner-101"}, {404: "owner-404"}, {101: ""}, {101: " owner-101"}} {
		_, err := NewWithAttestedOwners(t.Context(), nil, "synthetic-token", owners)
		require.Error(t, err)
	}
	fake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	for sender, expected := range map[int64]string{101: "alice", 202: "bob", 303: "visitor"} {
		owner, known := fake.domainOwner(sender)
		require.True(t, known)
		require.Equal(t, expected, owner)
	}
	_, known := fake.domainOwner(404)
	require.False(t, known)
	require.False(t, attestedModelOwner(map[int64]string{404: "owner-404"}, "owner-404"))
}

func TestAttestedSandboxRemoteAndPersistedConsumptionKeepExactOwner(t *testing.T) {
	t.Parallel()
	owners := map[int64]string{101: "owner-101"}
	fake, err := NewWithAttestedOwners(t.Context(), nil, "synthetic-token", owners)
	require.NoError(t, err)
	update, err := fake.installAndEnqueueFixture(t.Context(), modelFixtureInstall{
		Input: &modelFixtureInput{User: 101, Text: "hello"},
		Steps: []modelFixtureStep{fixtureStep("hello")},
	})
	require.NoError(t, err)
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	remote := FixtureRemote{URL: server.URL + "/lab/model", HTTP: server.Client(), AttestedOwners: owners}
	for _, owner := range []string{"alice", "foreign-owner"} {
		ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: owner, UpdateID: update})
		_, err = remote.Plan(ctx, agent.Input{Text: "hello"})
		require.Error(t, err)
	}
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "owner-101", UpdateID: update})
	plan, err := remote.Plan(ctx, agent.Input{Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, "Synthetic answer", plan.Text)
	restored, err := NewWithAttestedOwners(t.Context(), nil, "synthetic-token", owners)
	require.NoError(t, err)
	require.NoError(t, restored.restoreModelConsumption(fake.modelConsumptionSnapshot()))
	require.Error(t, restored.installModelCase(t.Context(), modelFixtureInstall{
		Owner: "owner-101", UpdateID: update, Steps: []modelFixtureStep{fixtureStep("hello")},
	}))
	defaultFake, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	require.Error(t, defaultFake.restoreModelConsumption(fake.modelConsumptionSnapshot()))
	require.Error(t, restored.restoreModelConsumption(&modelConsumptionSnapshot{
		Version: 1,
		Rows: []modelConsumption{{Owner: "alice", UpdateID: update, RequestSHA256: strings.Repeat("0", 64),
			ResponseSHA256: strings.Repeat("0", 64)}},
	}))
}
