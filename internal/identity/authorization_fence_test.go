package identity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/identity"
)

func TestInvalidationFenceRejectsAffectedOutstandingAuthority(t *testing.T) {
	t.Parallel()
	credential := identity.ClientCredentials{ID: "fixture", Secret: "fixture"}
	client, err := identity.New(
		identity.Options{
			Issuer:                         "http://127.0.0.1:1",
			OrganizationID:                 "org",
			BotID:                          123,
			LoomAudience:                   "loom",
			APIAudience:                    "api",
			ActorID:                        "actor",
			ActorClientCredentials:         credential,
			ExchangeClientCredentials:      credential,
			IntrospectionClientCredentials: credential,
		},
		nil,
	)
	require.NoError(t, err)
	before := client.InvalidationFence("user")
	require.True(t, before())
	client.Invalidate("other")
	require.True(t, before())
	client.Invalidate("user")
	require.False(t, before())
	after := client.InvalidationFence("user")
	require.True(t, after())
	client.Invalidate("actor")
	require.False(t, after())
	require.True(t, client.InvalidationFence("user")())
}
