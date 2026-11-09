//nolint:testpackage // Verify the caller's disabled-bridge path without a database connection.
package task

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

func TestProfileIAMAccountDisabledPinsKeepSyntheticPathWithoutDB(t *testing.T) {
	t.Parallel()
	credentials := identity.ClientCredentials{ID: "synthetic-client", Secret: "synthetic-secret"}
	iam, err := identity.New(identity.Options{
		Issuer: "http://127.0.0.1", OrganizationID: "org", BotID: 123,
		LoomAudience: "loom", APIAudience: "api", ActorID: "actor",
		ActorClientCredentials: credentials, ExchangeClientCredentials: credentials,
		IntrospectionClientCredentials: credentials,
	}, nil)
	require.NoError(t, err)
	service := &Service{iam: iam} // A DB borrow would panic, even before any IAM request.
	scope := db.ProfileScope{BotID: 123, Organization: "org", Subject: "subject", UserID: uuid.New()}
	account, err := service.profileIAMAccount(t.Context(), scope, db.User{Subject: "subject", TelegramUserID: 456})
	require.NoError(t, err)
	require.Equal(t, "subject", account.Subject)
	require.Equal(t, "tg+123+456@telegram.invalid", account.SyntheticEmail)
	require.Nil(t, account.TelegramLink)
}
