package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZitadelConfigurationIsExplicit(t *testing.T) {
	t.Parallel()
	valid := Auth{Mode: "zitadel", Zitadel: Zitadel{
		Issuer: "http://localhost:8113", Audience: "project", BotID: "123",
		BotClientID: "bot", BotClientSecret: "bot-secret",
		APIClientID: "api", APIClientSecret: "api-secret",
		ActorID: "actor", ActorClientID: "actor-client", ActorClientSecret: "actor-secret",
	}}
	require.NoError(t, valid.validate(sandboxMode))
	require.NoError(t, (Auth{}).validate(sandboxMode))
	for _, mutate := range []func(*Auth){
		func(a *Auth) { a.Mode = "" },
		func(a *Auth) { a.Mode = "typo" },
		func(a *Auth) { a.Zitadel.BotID = "0" },
		func(a *Auth) { a.Zitadel.BotID = "invalid" },
		func(a *Auth) { a.Zitadel.APIClientID = a.Zitadel.BotClientID },
		func(a *Auth) { a.Zitadel.ActorClientSecret = "" },
	} {
		value := valid
		mutate(&value)
		require.Error(t, value.validate(sandboxMode))
	}
}

func TestSandboxTelegramOwnersRequireExplicitSandbox(t *testing.T) {
	t.Parallel()
	valid := Auth{
		Mode:                  sandboxMode,
		SandboxTelegramOwners: map[int64]string{101: "owner-101", 202: "owner-202", 303: "owner-303"},
	}
	require.NoError(t, valid.validate(sandboxMode))
	for _, test := range []struct {
		name string
		env  string
		auth Auth
	}{
		{"production", productionMode, valid},
		{"implicit mode", sandboxMode, Auth{SandboxTelegramOwners: valid.SandboxTelegramOwners}},
		{"zitadel", sandboxMode, Auth{Mode: zitadelMode, SandboxTelegramOwners: valid.SandboxTelegramOwners}},
		{"empty mapping", sandboxMode, Auth{Mode: sandboxMode, SandboxTelegramOwners: map[int64]string{}}},
		{"invalid sender", sandboxMode, Auth{Mode: sandboxMode, SandboxTelegramOwners: map[int64]string{0: "owner-101"}}},
		{"empty owner", sandboxMode, Auth{Mode: sandboxMode, SandboxTelegramOwners: map[int64]string{101: ""}}},
		{"padded owner", sandboxMode, Auth{Mode: sandboxMode, SandboxTelegramOwners: map[int64]string{101: " owner-101"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Error(t, test.auth.validate(test.env))
		})
	}
	var cfg Config
	require.NoError(
		t,
		decodeYAML([]byte("auth:\n  mode: sandbox\n  sandbox_telegram_owners:\n    101: owner-101\n"), &cfg),
	)
	require.Equal(t, "owner-101", cfg.Auth.SandboxTelegramOwners[101])
	require.NoError(t, cfg.Auth.validate(sandboxMode))
	for _, mapping := range []string{
		"101: owner-101\n    101: owner-202",
		"101: 202",
		"'101': owner-101",
		"unknown: owner-101",
		"0: owner-101",
		"101: &owner owner-101",
	} {
		require.Error(
			t,
			decodeYAML([]byte("auth:\n  mode: sandbox\n  sandbox_telegram_owners:\n    "+mapping+"\n"), &Config{}),
		)
	}
}
