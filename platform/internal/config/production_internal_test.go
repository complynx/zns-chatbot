package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func productionConfig(command string) Config {
	c := defaults(command)
	c.Env = productionMode
	c.Database.URL = "postgres://app:unique-database-password@postgres/zns?sslmode=disable"
	c.Auth = Auth{Mode: "zitadel", SigningKey: Secret(strings.Repeat("k", 32)), Zitadel: Zitadel{
		Issuer: "https://identity.example.com", Audience: "project", BotID: "123",
		BotClientID: "bot", BotClientSecret: "independent-bot-secret",
		APIClientID: "api", APIClientSecret: "independent-api-secret",
		ActorID: "actor", ActorClientID: "actor-client", ActorClientSecret: "independent-actor-secret",
	}}
	c.Telegram.Token = "123:independent-telegram-secret"
	c.Telegram.BaseURL = "https://api.telegram.org"
	c.Telegram.WebAppURL = "https://bot.example.com/miniapp/"
	c.Model.Provider = "openai"
	c.Model.OpenAIKey = "independent-provider-key"
	c.Core.URL = "http://api:8080"
	c.Orders.ActiveEvent = "festival"
	return c
}

func TestProductionCommands(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"app", "api", "bot", "migrate", "health"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, productionConfig(command).Validate(command))
		})
	}
	for _, command := range []string{"fake", "fixture", "product-fixture", "export-fixture", "model"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, productionConfig(command).Validate(command), "production")
		})
	}
	minimal := defaults("migrate")
	minimal.Env = productionMode
	minimal.Database.URL = productionConfig("migrate").Database.URL
	require.NoError(t, minimal.Validate("migrate"), "migrator does not need runtime identity credentials")
	minimal.Database.URL = ""
	require.NoError(t, minimal.Validate("health"), "health performs no business operations")
}

func TestProductionIdentityRestrictions(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Config){
		"sandbox auth":        func(c *Config) { c.Auth = Auth{SigningKey: c.Auth.SigningKey} },
		"http issuer":         func(c *Config) { c.Auth.Zitadel.Issuer = "http://identity.example.com" },
		"fixture signer":      func(c *Config) { c.Auth.SigningKey = "sandbox-only-do-not-use-in-production-123456789" },
		"fixture actor":       func(c *Config) { c.Auth.Zitadel.ActorClientSecret = "synthetic-actor-secret" },
		"missing bot secret":  func(c *Config) { c.Auth.Zitadel.BotClientSecret = "" },
		"short api secret":    func(c *Config) { c.Auth.Zitadel.APIClientSecret = "short" },
		"http public url":     func(c *Config) { c.Telegram.WebAppURL = "http://bot.example.com" },
		"loopback public url": func(c *Config) { c.Telegram.WebAppURL = "https://127.0.0.1/miniapp" },
		"private public url":  func(c *Config) { c.Telegram.WebAppURL = "https://192.168.1.1/miniapp" },
		"localhost origin":    func(c *Config) { c.Auth.LegacyBrowserOrigins = "https://localhost" },
		"synthetic flag":      func(c *Config) { c.SyntheticOnly = true },
		"parent stdin":        func(c *Config) { c.ParentStdin = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := productionConfig("app")
			mutate(&c)
			require.Error(t, c.Validate("app"))
		})
	}
	c := productionConfig("api")
	assert.False(t, c.Auth.Zitadel.AdapterConfig(c.Env).Sandbox)
	assert.True(t, c.Auth.Zitadel.AdapterConfig(sandboxMode).Sandbox)
}

func TestProductionProviderRestrictions(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Config){
		"default telegram token":    func(c *Config) { c.Telegram.Token = "sandbox" },
		"wrong telegram bot":        func(c *Config) { c.Telegram.Token = "124:independent-telegram-secret" },
		"fixture telegram token":    func(c *Config) { c.Telegram.Token = "123:synthetic-telegram-secret" },
		"fake telegram endpoint":    func(c *Config) { c.Telegram.BaseURL = "https://fake.example.com" },
		"missing telegram endpoint": func(c *Config) { c.Telegram.BaseURL = "" },
		"fixture openai key":        func(c *Config) { c.Model.OpenAIKey = "sandbox-only-synthetic-asr" },
		"scripted model":            func(c *Config) { c.Model.Provider = "scripted" },
		"fixture model":             func(c *Config) { c.Model.Provider = "fixture" },
		"codex model":               func(c *Config) { c.Model.Provider = "codex" },
		"remote model":              func(c *Config) { c.Model.Provider = "remote"; c.Model.URL = "http://model:8080" },
		"fixture worker secret": func(c *Config) {
			c.Media = Worker{URL: "http://media:8080", Secret: "sandbox-product-media-only"}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := productionConfig("app")
			mutate(&c)
			require.Error(t, c.Validate("app"))
		})
	}
}

func TestProductionDatabaseRestrictions(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"postgres://postgres:sandbox-owner-only@postgres/zns",
		"postgres://app@postgres/zns", "postgres://app:short@postgres/zns",
		"host=postgres user=app password=independent-password dbname=zns", "malformed",
		"postgres://app:independent-password@postgres/zns?password=sandbox-product-only",
	} {
		c := productionConfig("migrate")
		c.Database.URL = Secret(dsn)
		err := c.Validate("migrate")
		require.ErrorContains(t, err, "production database")
		assert.NotContains(t, err.Error(), dsn)
	}
}

func TestProductionAPIModelRestrictions(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"remote", "scripted", "fixture", "codex"} {
		c := productionConfig("api")
		c.Model.Provider = provider
		require.ErrorContains(t, c.Validate("api"), "production runtime requires openai")
	}
	c := productionConfig("api")
	c.Model.OpenAIKey = ""
	require.ErrorContains(t, c.Validate("api"), "production runtime requires openai")
}
