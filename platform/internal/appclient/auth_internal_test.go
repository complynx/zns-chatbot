package appclient

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestAttestedSandboxOwnersBindVerifiedSenderAndNotification(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte("synthetic-attested-owner-key-at-least-32")}
	client := Client{SandboxToken: signer.Token, SandboxTelegramOwners: map[int64]string{
		101: "owner-101", 202: "owner-202", 303: "owner-303",
	}}
	for sender, expected := range client.SandboxTelegramOwners {
		ctx, owner, err := client.AuthenticateTelegram(t.Context(), sender)
		require.NoError(t, err)
		require.Equal(t, expected, owner)
		token, err := client.UserToken(ctx, owner)
		require.NoError(t, err)
		verified, err := signer.Verify(token)
		require.NoError(t, err)
		require.Equal(t, expected, verified)
		_, err = client.UserToken(ctx, "foreign-owner")
		require.ErrorIs(t, err, identity.ErrZitadelIdentity)
		notice, err := client.NotificationContext(t.Context(), expected, sender)
		require.NoError(t, err)
		_, err = client.UserToken(notice, expected)
		require.NoError(t, err)
		_, err = client.NotificationContext(t.Context(), "foreign-owner", sender)
		require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	}
	_, _, err := client.AuthenticateTelegram(t.Context(), 404)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	_, err = client.UserToken(t.Context(), "owner-101")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	delete(client.SandboxTelegramOwners, 202)
	_, _, err = client.AuthenticateTelegram(t.Context(), 202)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
}

func TestDefaultSandboxActorsAndConfiguredIdentityFailure(t *testing.T) {
	t.Parallel()
	client := Client{}
	for sender, expected := range map[int64]string{101: "alice", 202: "bob", 303: "visitor"} {
		_, owner, err := client.AuthenticateTelegram(t.Context(), sender)
		require.NoError(t, err)
		require.Equal(t, expected, owner)
	}
	_, _, err := client.AuthenticateTelegram(t.Context(), 404)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	denied := failingIdentity{err: identity.ErrZitadelIdentity}
	client = Client{SandboxTelegramOwners: map[int64]string{101: "owner-101"}, Links: denied, Exchange: denied}
	_, _, err = client.AuthenticateTelegram(t.Context(), 101)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	client.Exchange = nil
	_, _, err = client.AuthenticateTelegram(t.Context(), 101)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
}
