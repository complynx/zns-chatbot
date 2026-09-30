package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

const managedFixtureEndpoint = "http://fake:8080/lab/model"
const managedAgentStandDirectory = "../../../docs/sandbox/managed-agent-stand/"

func TestManagedFixtureModelBoundary(t *testing.T) {
	t.Parallel()
	valid := config.Config{Env: "sandbox", SyntheticOnly: true,
		Model: config.Model{Provider: fixtureMode, URL: managedFixtureEndpoint}}
	assert.True(t, managedModelAllowed(valid))
	for _, environment := range []string{"production", "", "development"} {
		changed := valid
		changed.Env = environment
		assert.False(t, managedModelAllowed(changed), environment)
	}
	changed := valid
	changed.SyntheticOnly = false
	assert.False(t, managedModelAllowed(changed))
	for _, endpoint := range []string{
		"", "https://fake:8080/lab/model", "http://fake:8080/lab/model/",
		"http://fake:8080/lab/model?scope=alice", "http://fake:8080/lab/model#fragment",
		"http://user:pass@fake:8080/lab/model", "http://outside:8080/lab/model",
	} {
		changed = valid
		changed.Model.URL = endpoint
		assert.False(t, managedModelAllowed(changed), endpoint)
	}
	for _, provider := range []string{"scripted", "remote", "codex", ""} {
		changed = valid
		changed.Model.Provider = provider
		assert.False(t, managedModelAllowed(changed), provider)
	}
	assert.True(t, managedModelAllowed(config.Config{
		Env: productionEnvironment, Model: config.Model{Provider: openAIProvider},
	}))
}

func TestManagedFixtureStandFactory(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(managedAgentStandDirectory + "runtime.yaml")
	require.NoError(t, err)
	compose, err := os.ReadFile(managedAgentStandDirectory + "runtime.compose.yaml")
	require.NoError(t, err)
	var project struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(compose, &project))
	require.Len(t, project.Services, 6)
	for _, component := range []string{
		"app", "evaluator", "media-decoder", "media-broker", "sticker-decoder", "sticker-broker",
	} {
		require.Contains(t, project.Services, component)
	}
	environment := make([]string, 0, len(project.Services["app"].Environment)+1)
	for name, value := range project.Services["app"].Environment {
		environment = append(environment, name+"="+value)
	}
	// Config validates native absolute paths; the deployed stand uses Linux's
	// /run socket while this factory check can run on Windows.
	environment = append(environment, "ZNS_SCRIPT__SOCKET="+filepath.Join(t.TempDir(), "evaluate.sock"))
	cfg, err := config.Load("app", data, configEnvironment(environment))
	require.NoError(t, err)
	require.True(t, managedModelAllowed(cfg))
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, runtime.Shutdown(context.Background())) })
	model, err := unobservedModel(cfg, runtime)
	require.NoError(t, err)
	fixture, ok := model.(sandbox.FixtureRemote)
	require.True(t, ok, "stand must use the existing scoped fixture adapter")
	assert.Equal(t, managedFixtureEndpoint, fixture.URL)
	cfg.Env = productionEnvironment
	require.Error(t, cfg.Validate("app"))
	assert.False(t, managedModelAllowed(cfg))
}

type standFixtureCase struct {
	Input struct {
		Text         string `json:"text"`
		LanguageCode string `json:"language_code"`
	} `json:"input"`
	Steps []struct {
		Expect agent.Input `json:"expect"`
		Plan   agent.Plan  `json:"plan"`
	} `json:"steps"`
}

func TestManagedFixtureStandScenarios(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(managedAgentStandDirectory + "scenarios." + locale + ".json")
			require.NoError(t, err)
			var scenarios []json.RawMessage
			require.NoError(t, json.Unmarshal(data, &scenarios))
			require.Len(t, scenarios, 3)
			fake, err := sandbox.New(t.Context(), nil, "999:sandbox")
			require.NoError(t, err)
			server := httptest.NewServer(fake.Handler())
			t.Cleanup(server.Close)
			model := sandbox.FixtureRemote{URL: server.URL + "/lab/model", HTTP: server.Client()}
			for _, scenario := range scenarios {
				checkManagedFixtureCase(t, server.URL, model, locale, scenario)
			}
		})
	}
}

func checkManagedFixtureCase(
	t *testing.T, base string, model sandbox.FixtureRemote, locale string, raw json.RawMessage,
) {
	t.Helper()
	var scenario standFixtureCase
	require.NoError(t, json.Unmarshal(raw, &scenario))
	require.Equal(t, locale, scenario.Input.LanguageCode)
	require.Len(t, scenario.Steps, 1)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		base+"/lab/model/fixtures", bytes.NewReader(raw))
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	response, err := model.HTTP.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(data))
	var installed struct {
		UpdateID int64 `json:"update_id"`
	}
	require.NoError(t, json.Unmarshal(data, &installed))
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: installed.UpdateID})
	input := scenario.Steps[0].Expect
	require.Equal(t, scenario.Input.Text, input.Text)
	denied := errors.New("synthetic authorization denied")
	input.BeforeProvider = func(context.Context, *agent.Input) error { return denied }
	_, err = model.Plan(ctx, input)
	require.ErrorIs(t, err, denied, "authorization denial must not consume the fixture")
	input.BeforeProvider = nil
	plan, err := model.Plan(ctx, input)
	require.NoError(t, err)
	assert.Equal(t, scenario.Steps[0].Plan, plan)
	_, err = model.Plan(ctx, input)
	require.Error(t, err, "consumed pre-save model turns are explicitly not replayable")
}
