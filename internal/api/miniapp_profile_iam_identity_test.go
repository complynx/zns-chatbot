//nolint:testpackage // Verify the private callback helper does not borrow DB when prelink is disabled.
package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

func TestMiniAppIAMProfileAccountDisabledPinsKeepSyntheticPathWithoutDB(t *testing.T) {
	t.Parallel()
	s := &Server{} // No store or IAM requests are required in the disabled bridge path.
	scope := db.ProfileScope{BotID: 123, Organization: "org", Subject: "subject", UserID: uuid.New()}
	account, err := s.miniAppIAMProfileAccount(t.Context(), scope, db.User{Subject: "subject", TelegramUserID: 456})
	require.NoError(t, err)
	require.Equal(t, "subject", account.Subject)
	require.Equal(t, "tg+123+456@telegram.invalid", account.SyntheticEmail)
	require.Nil(t, account.TelegramLink)
}

func TestMiniAppIAMProfileAccountUnusablePinsNeverBorrowDB(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, issuer, provider string
	}{
		{"missing issuer", "", "provider"},
		{"missing provider", "https://provider.example", ""},
		{"insecure issuer", "http://provider.example", "provider"},
		{"invalid provider", "https://provider.example", "invalid\nprovider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			credentials := identity.ClientCredentials{ID: "synthetic-client", Secret: "synthetic-secret"}
			iam, err := identity.New(identity.Options{
				Issuer: "http://127.0.0.1", OrganizationID: "org", BotID: 123,
				LoomAudience: "loom", APIAudience: "api", ActorID: "actor",
				ActorClientCredentials: credentials, ExchangeClientCredentials: credentials,
				IntrospectionClientCredentials: credentials,
				TelegramProviderIssuer:         test.issuer,
				TelegramProviderID:             test.provider,
			}, nil)
			require.NoError(t, err)
			// Raw API options remain populated; only normalized IAM pins can enable evidence reads.
			s := &Server{iam: iam, options: Options{TelegramPrelink: TelegramPrelinkOptions{
				Issuer: test.issuer, ProviderID: test.provider,
			}}}
			scope := db.ProfileScope{BotID: 123, Organization: "org", Subject: "subject", UserID: uuid.New()}
			account, err := s.miniAppIAMProfileAccount(t.Context(), scope,
				db.User{Subject: "subject", TelegramUserID: 456})
			require.NoError(t, err)
			require.Equal(t, "tg+123+456@telegram.invalid", account.SyntheticEmail)
			require.Nil(t, account.TelegramLink)
		})
	}
}
