package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const diagnosticChild = "GO_CONFIG_DIAGNOSTIC_CHILD"
const diagnosticCanary = "CANARY-DO-NOT-LOG-7a9db625"

const diagnosticConfig = `
env: production
database:
  url: postgres://app:CANARY-DO-NOT-LOG-7a9db625@127.0.0.1:1/zns
auth:
  mode: zitadel
  signing_key: CANARY-DO-NOT-LOG-7a9db625-signing-key
  zitadel:
    issuer: https://identity.example.com
    audience: project
    bot_id: "123"
    bot_client_id: bot
    bot_client_secret: CANARY-DO-NOT-LOG-7a9db625-bot
    api_client_id: api
    api_client_secret: CANARY-DO-NOT-LOG-7a9db625-api
    actor_id: actor
    actor_client_id: actor-client
    actor_client_secret: CANARY-DO-NOT-LOG-7a9db625-actor
telegram:
  base_url: https://api.telegram.org
  token: "123:CANARY-DO-NOT-LOG-7a9db625-telegram"
  web_app_url: https://bot.example.com/miniapp/
core:
  url: http://127.0.0.1:1
orders:
  active_event: festival
model:
  provider: openai
  openai_key: CANARY-DO-NOT-LOG-7a9db625-provider
`

// The subprocess executes the real startup logger and exit path, before any
// provider or database operation. Its environment contains only test settings.
func TestProductionDiagnosticChild(t *testing.T) {
	t.Parallel()
	if command := os.Getenv(diagnosticChild); command != "" {
		os.Args[1] = command
		main()
	}
}

func TestProductionStartupDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, command, override, code string
	}{
		{"empty issuer", "api", "ZNS_AUTH__ZITADEL__ISSUER=", "production_zitadel_configuration"},
		{"http issuer", "api", "ZNS_AUTH__ZITADEL__ISSUER=http://127.0.0.1", "production_zitadel_configuration"},
		{"synthetic", "api", "ZNS_SYNTHETIC_ONLY=true", "production_synthetic"},
		{"parent stdin", "app", "ZNS_PARENT_STDIN=true", "production_synthetic"},
		{"fixture model", "api", "ZNS_MODEL__PROVIDER=fixture", "production_model_provider"},
		{"remote model", "api", "ZNS_MODEL__PROVIDER=remote", "production_model_provider"},
		{"scripted model", "app", "ZNS_MODEL__PROVIDER=scripted", "production_model_provider"},
		{"codex model", "app", "ZNS_MODEL__PROVIDER=codex", "production_model_provider"},
		{"sandbox auth", "api", "ZNS_AUTH__MODE=sandbox", "auth_configuration"},
		{"fake endpoint", "app", "ZNS_TELEGRAM__BASE_URL=http://fake:8080", "production_telegram_origin"},
		{"relative endpoint", "app", "ZNS_TELEGRAM__BASE_URL=/api.telegram.org", "production_telegram_origin"},
		{"namespace conflict", "app", "ZNS_AUTH__ZITADEL__BOT_ID=124", "auth_bot_namespace"},
		{"private webapp", "app", "ZNS_TELEGRAM__WEB_APP_URL=https://127.0.0.1/app", "production_public_url"},
		{"missing signing", "api", "ZNS_AUTH__SIGNING_KEY=", "production_signing_secret"},
		{"short signing", "api", "ZNS_AUTH__SIGNING_KEY=1234567890123456", "auth_signing_length"},
		{"fake command", "fake", "", "production_command"},
		{"fixture command", "fixture", "", "production_command"},
		{"product fixture", "product-fixture", "", "production_command"},
		{"export fixture", "export-fixture", "", "production_command"},
		{"model command", "model", "", "production_model_boundary"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output := diagnosticStartup(t, test.command, diagnosticConfig, test.override)
			var entry struct {
				Error struct {
					Code   string `json:"code"`
					Field  string `json:"field"`
					Reason string `json:"reason"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(output, &entry))
			assert.Equal(t, test.code, entry.Error.Code)
			assert.NotEmpty(t, entry.Error.Field)
			assert.NotEmpty(t, entry.Error.Reason)
			assert.NotContains(t, entry.Error.Reason, "[redacted]")
		})
	}
}

func TestMalformedStartupConfigurationStaysRedacted(t *testing.T) {
	t.Parallel()
	output := diagnosticStartup(t, "api", "auth: ["+diagnosticCanary, "")
	var entry struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(output, &entry))
	assert.Equal(t, "operation failed", entry.Error)
}

func diagnosticStartup(t *testing.T, command, data, override string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestProductionDiagnosticChild$")
	for _, key := range []string{"SystemRoot", "SystemDrive", "TEMP", "TMP", "PATH"} {
		if value, present := os.LookupEnv(key); present {
			child.Env = append(child.Env, key+"="+value)
		}
	}
	child.Env = append(child.Env, diagnosticChild+"="+command, "ZNS_CONFIG_FILE="+path)
	if override != "" {
		child.Env = append(child.Env, override)
	}
	output, err := child.CombinedOutput()
	var exitError *exec.ExitError
	require.ErrorAs(t, err, &exitError)
	require.Equal(t, 1, exitError.ExitCode(), "%s", output)
	require.NotContains(t, string(output), diagnosticCanary)
	return output
}
