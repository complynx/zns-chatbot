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
