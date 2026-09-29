package config_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func botEnvironment() []string {
	return []string{
		"ZNS_ENV=sandbox", "DATABASE_URL=postgres://localhost/test", "SANDBOX_SIGNING_KEY=" + strings.Repeat("k", 32),
		"CORE_URL=http://core:8080", "MODEL_URL=http://model:8080",
	}
}

func TestConfigurationPrecedenceAndLegacy(t *testing.T) {
	t.Parallel()
	data := []byte(
		"server:\n  port: 8100\n  read_timeout: 21s\nsticker:\n  cache:\n    capacity: 31\n    half_life: 48h\n",
	)
	environment := append(botEnvironment(), "PORT=8200", "ZNS_SERVER__PORT=8300", "ZNS_STICKER__CACHE__CAPACITY=41")
	for _, reverse := range []bool{false, true} {
		t.Run(strconv.FormatBool(reverse), func(t *testing.T) {
			t.Parallel()
			values := append([]string(nil), environment...)
			if reverse {
				for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
					values[i], values[j] = values[j], values[i]
				}
			}
			result, err := config.Load("bot", data, values)
			require.NoError(t, err)
			assert.Equal(t, 8300, result.Server.Port)
			assert.Equal(t, 21*time.Second, result.Server.ReadTimeout)
			assert.Equal(t, 41, result.Sticker.Cache.Capacity)
			assert.Equal(t, 48*time.Hour, result.Sticker.Cache.HalfLife)
			assert.Equal(t, 5*time.Second, result.Shutdown.Drain)
			assert.Equal(t, "sandbox", result.Telegram.Token.Value())
			assert.Equal(t, "remote", result.Model.Provider)
			assert.False(t, result.OTel.Enabled)
		})
	}
}

func TestConfigurationRejectsAmbiguousYAML(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"unknown": "server:\n  porrt: 8080\n", "duplicate": "server:\n  port: 8000\n  port: 9000\n",
		"document": "{}\n---\n{}\n", "null": "server: null\n", "string int": "server: {port: '8080'}",
		"integer duration": "shutdown: {drain: 5}", "number secret": "auth: {signing_key: 123}",
		"legacy bool": "otel: {enabled: yes}", "alias": "server: &server {port: 8080}\ncore: *server",
		"merge": "server: {<<: {port: 8080}}", "tag": "env: !custom sandbox", "sequence": "[env, sandbox]",
		"extra nesting": "server: {port: {inner: 1}}", "invalid": "server: [",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load("bot", []byte(data), botEnvironment())
			require.Error(t, err)
		})
	}
	_, err := config.Load("bot", []byte(strings.Repeat(" ", 1<<20+1)), botEnvironment())
	require.ErrorContains(t, err, "1 MiB")
}

func TestConfigurationEnvironmentValidation(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{
		"ZNS_UNKNOWN=value", "ZNS_SERVER__PORT=", "ZNS_SERVER__PORT=65536", "ZNS_SERVER__PORT=-1", "ZNS_SERVER__PORT=1.5",
		"ZNS_SHUTDOWN__DRAIN=0s", "ZNS_ORDERS__REMINDER_AFTER=-1s", "ZNS_STICKER__CACHE__HALF_LIFE=500ms",
		"ZNS_STICKER__CACHE__CAPACITY=0", "ZNS_OTEL__SAMPLE_RATIO=NaN", "ZNS_OTEL__SAMPLE_RATIO=Inf", "ZNS_OTEL__SAMPLE_RATIO=1.1",
		"ZNS_OTEL__ENABLED=yes", "ZNS_PARENT_STDIN=1", "ZNS_MEDIA__SECRET=only-secret", "ZNS_TELEGRAM__TOKEN=",
		"ZNS_SERVER__PORT", "ZNS_SERVER__HOST=http://localhost",
	} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load("bot", nil, append(botEnvironment(), entry))
			require.Error(t, err)
		})
	}
	_, err := config.Load("bot", nil, append(botEnvironment(), "ZNS_SERVER__PORT=8000", "ZNS_SERVER__PORT=9000"))
	require.ErrorContains(t, err, "duplicate")
	result, err := config.Load(
		"bot",
		nil,
		append(
			botEnvironment(),
			"PORT=",
			"UNRELATED_SETTING=ignored",
			"ZNS_SERVER__HOST=localhost",
			"ZNS_CONFIG_FILE=operator.yaml",
		),
	)
	require.NoError(t, err)
	assert.Equal(t, 8080, result.Server.Port)
}

func TestConfigurationModeRequirements(t *testing.T) {
	t.Parallel()
	_, err := config.Load("health", nil, nil)
	require.NoError(t, err)
	_, err = config.Load("api", nil, []string{"ZNS_ENV=sandbox", "DATABASE_URL=postgres://localhost/test"})
	require.ErrorContains(t, err, "auth.signing_key")
	_, err = config.Load("migrate", nil, []string{"ZNS_ENV=sandbox"})
	require.ErrorContains(t, err, "database.url")
	_, err = config.Load("migrate", nil, []string{"DATABASE_URL=postgres://localhost/test"})
	require.ErrorContains(t, err, "env")
	_, err = config.Load("migrate", nil, []string{"ZNS_ENV=production", "DATABASE_URL=postgres://localhost/test"})
	require.ErrorContains(t, err, "production database")
	_, err = config.Load("model", nil, []string{"ZNS_ENV=sandbox", "MODEL_PROVIDER=scripted"})
	require.NoError(t, err)
	_, err = config.Load("model", nil, []string{"ZNS_ENV=sandbox"})
	require.ErrorContains(t, err, "model.openai_key")
	model, err := config.Load(
		"model",
		nil,
		[]string{
			"ZNS_ENV=sandbox",
			"OPENAI_API_KEY=synthetic-test-only",
			"ZNS_MODEL__REMOTE_SECRET=synthetic-channel-secret",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "openai", model.Model.Provider)
	_, err = config.Load("unknown", nil, nil)
	require.ErrorContains(t, err, "unknown")
	appEnv := append(botEnvironment(), "MODEL_PROVIDER=scripted", "ZNS_CORE__URL=")
	_, err = config.Load("app", nil, appEnv)
	require.NoError(t, err)
	_, err = config.Load("bot", nil, appEnv)
	require.Error(t, err)
}

func TestConfigurationWorkerAndTelemetry(t *testing.T) {
	t.Parallel()
	data := []byte(
		"media:\n  url: http://worker:8091\n  secret: test-only\nsticker:\n  worker:\n    url: http://sticker:8098\n    secret: test-only\notel:\n  enabled: true\n  endpoint: http://collector:4318\n  sample_ratio: 0.25\nshutdown:\n  drain: 30s\n  telemetry_flush: 3s\n",
	)
	result, err := config.Load("bot", data, botEnvironment())
	require.NoError(t, err)
	assert.True(t, result.OTel.Enabled)
	assert.InDelta(t, 0.25, result.OTel.SampleRatio, 0)
	assert.Equal(t, 30*time.Second, result.Shutdown.Drain)
	for _, entry := range []string{"ZNS_MEDIA__URL=http://worker/path", "ZNS_MEDIA__URL=http://user:password@worker", "ZNS_MEDIA__SECRET=\n", "ZNS_OTEL__ENDPOINT=", "ZNS_OTEL__ENDPOINT=http://user:password@collector"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			_, loadErr := config.Load("bot", data, append(botEnvironment(), entry))
			require.Error(t, loadErr)
		})
	}
}

func TestConfigurationCodexBoundaries(t *testing.T) {
	t.Parallel()
	executable := filepath.Join(t.TempDir(), "codex")
	env := append(
		botEnvironment(),
		"MODEL_PROVIDER=codex",
		"CODEX_EXECUTABLE="+executable,
		"BIND_HOST=127.0.0.1",
		"SYNTHETIC_ONLY=true",
	)
	_, err := config.Load("bot", nil, env)
	require.NoError(t, err, "loader validates path shape without probing executables")
	for _, entry := range []string{"ZNS_SYNTHETIC_ONLY=false", "ZNS_SERVER__HOST=0.0.0.0", "ZNS_MODEL__CODEX_EXECUTABLE=relative"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			_, loadErr := config.Load("bot", nil, append(slices.Clone(env), entry))
			require.Error(t, loadErr)
		})
	}
}

func TestConfigurationSecretErrorsAndFormatting(t *testing.T) {
	t.Parallel()
	const secret = "never-print-this-secret"
	_, err := config.Load("bot", []byte("server:\n  port: "+secret), botEnvironment())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
	_, err = config.Load("bot", nil, append(botEnvironment(), "ZNS_SERVER__PORT="+secret))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
	result, err := config.Load("bot", nil, append(botEnvironment(), "ZNS_MODEL__OPENAI_KEY="+secret))
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secret)
	yamlData, err := yaml.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(yamlData), secret)
	assert.NotContains(t, fmt.Sprintf("%+v %#v", result, result), secret)
	assert.Equal(t, slog.StringValue("[redacted]"), result.Model.OpenAIKey.LogValue())
	assert.Equal(t, secret, result.Model.OpenAIKey.Value())
}
