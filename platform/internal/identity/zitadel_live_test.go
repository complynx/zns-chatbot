package identity_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// TestZitadelLocalAdapter uses only the explicitly enabled synthetic loopback
// stand. Credentials and tokens are never included in assertion output.
func TestZitadelLocalAdapter(t *testing.T) {
	t.Parallel()
	path := os.Getenv("ZITADEL_LOCAL_STATE")
	if path == "" {
		t.Skip("ZITADEL_LOCAL_STATE is required for synthetic identity acceptance")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	type app struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	type user struct {
		UserID string `json:"userId"`
	}
	var state struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		Bot         app  `json:"bot_app"`
		API         app  `json:"api_app"`
		ActorSecret app  `json:"actor_secret"`
		Actor       user `json:"actor"`
		Alice       user `json:"alice"`
		Bob         user `json:"bob"`
	}
	require.NoError(t, json.Unmarshal(data, &state))
	config := identity.ZitadelConfig{
		Issuer:            "http://localhost:8113",
		Sandbox:           true,
		Audience:          state.Project.ID,
		BotClientID:       state.Bot.ClientID,
		BotClientSecret:   state.Bot.ClientSecret,
		APIClientID:       state.API.ClientID,
		APIClientSecret:   state.API.ClientSecret,
		ActorID:           state.Actor.UserID,
		ActorClientID:     state.ActorSecret.ClientID,
		ActorClientSecret: state.ActorSecret.ClientSecret,
	}
	client, err := identity.NewZitadel(config)
	require.NoError(t, err)
	for _, subject := range []string{state.Alice.UserID, state.Bob.UserID} {
		token, exchangeErr := client.Exchange(t.Context(), subject)
		require.NoError(t, exchangeErr)
		verified, verifyErr := client.Verify(t.Context(), token)
		require.NoError(t, verifyErr)
		assert.Equal(t, subject, verified, "delegated subject must match")
	}
	_, err = client.Exchange(t.Context(), "nonexistent-synthetic")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	_, err = client.Verify(t.Context(), "invalid-synthetic")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	for _, defect := range []string{"actor_credentials", "api_credentials", "audience", "actor_claim"} {
		broken := config
		switch defect {
		case "actor_credentials":
			broken.ActorClientSecret = "invalid-synthetic"
		case "api_credentials":
			broken.APIClientSecret = "invalid-synthetic"
		case "audience":
			broken.Audience = "nonexistent-synthetic"
		case "actor_claim":
			broken.ActorID = "nonexistent-synthetic"
		}
		invalid, createErr := identity.NewZitadel(broken)
		require.NoError(t, createErr)
		token, denied := invalid.Exchange(t.Context(), state.Alice.UserID)
		if denied == nil {
			_, denied = invalid.Verify(t.Context(), token)
		}
		require.ErrorIs(t, denied, identity.ErrZitadelIdentity, defect)
	}
}
