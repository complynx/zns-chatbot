package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type productionTransport func(*http.Request) (*http.Response, error)

func (transport productionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestProductionConfigurationUsesOfficialTelegramOrigin(t *testing.T) {
	t.Parallel()
	c, err := config.Load("bot", []byte(`
env: production
database:
  url: postgres://app:independent-database-password@postgres/zns
auth:
  mode: zitadel
  signing_key: independently-issued-signing-key-32bytes
  zitadel:
    issuer: https://identity.example.com
    audience: project
    bot_id: "123"
    bot_client_id: bot
    bot_client_secret: independent-bot-secret
    api_client_id: api
    api_client_secret: independent-api-secret
    actor_id: actor
    actor_client_id: actor-client
    actor_client_secret: independent-actor-secret
telegram:
  base_url: https://api.telegram.org
  token: "123:independent-telegram-secret"
  web_app_url: https://bot.example.com/miniapp/
core:
  url: http://api:8080
orders:
  active_event: festival
model:
  provider: openai
  openai_key: independent-provider-key
`), nil)
	require.NoError(t, err)
	var called bool
	client := &http.Client{Transport: productionTransport(func(request *http.Request) (*http.Response, error) {
		called = true
		assert.Equal(t, "https", request.URL.Scheme)
		assert.Equal(t, "api.telegram.org", request.URL.Host)
		assert.Equal(t, "/bot"+c.Telegram.Token.Value()+"/getMe", request.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"id":123,"is_bot":true}}`))}, nil
	})}
	b := &bot.Bot{TG: telegram.Client{Base: c.Telegram.BaseURL, Token: c.Telegram.Token.Value(), HTTP: client}}
	require.NoError(t, verifyBotIdentity(t.Context(), b, c))
	assert.True(t, called)
}

func TestVerifyBotIdentityAtStartup(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/bottoken/getMe", r.URL.Path)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":123,"is_bot":true}}`))
	}))
	t.Cleanup(server.Close)
	b := &bot.Bot{TG: telegram.Client{Base: server.URL, Token: "token"}}
	c := config.Config{Env: "sandbox"}
	require.NoError(t, verifyBotIdentity(t.Context(), b, c))
	assert.Zero(t, calls.Load(), "sandbox must not acquire a new startup dependency")
	c.Env = "production"
	c.Auth.Zitadel.BotID = "invalid"
	require.ErrorContains(t, verifyBotIdentity(t.Context(), b, c), "invalid production")
	assert.Zero(t, calls.Load())
	c.Auth.Zitadel.BotID = "123"
	require.NoError(t, verifyBotIdentity(t.Context(), b, c))
	assert.EqualValues(t, 1, calls.Load())
	c.Auth.Zitadel.BotID = "456"
	require.ErrorContains(t, verifyBotIdentity(t.Context(), b, c), "does not match")
	assert.EqualValues(t, 2, calls.Load())
}
