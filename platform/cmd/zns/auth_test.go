package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestRuntimeAuthRejectsUnknownMode(t *testing.T) {
	t.Parallel()
	_, _, _, err := runtimeAuth(nil, config.Config{Auth: config.Auth{Mode: "typo"}}, identity.Signer{})
	require.EqualError(t, err, "invalid runtime authentication mode")
}

func TestRuntimeAuthRejectsSandboxOwnersOutsideExplicitSandbox(t *testing.T) {
	t.Parallel()
	for _, cfg := range []config.Config{
		{Env: "production", Auth: config.Auth{Mode: "sandbox"}},
		{Env: "sandbox", Auth: config.Auth{Mode: "zitadel"}},
		{Env: "sandbox", Auth: config.Auth{}},
	} {
		cfg.Auth.SandboxTelegramOwners = map[int64]string{101: "owner-101"}
		_, _, _, err := runtimeAuth(nil, cfg, identity.Signer{})
		require.EqualError(t, err, "sandbox Telegram owners require sandbox authentication")
	}
}

func TestZitadelSecretsAreRedacted(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Log: config.Log{Level: "info"}, Auth: config.Auth{Zitadel: config.Zitadel{
		BotClientSecret: "bot-private", APIClientSecret: "api-private", ActorClientSecret: "actor-private",
	}}}
	var output bytes.Buffer
	configuredLogger(&output, cfg).InfoContext(t.Context(), "bot-private api-private actor-private")
	assert.NotContains(t, output.String(), "-private")
}
