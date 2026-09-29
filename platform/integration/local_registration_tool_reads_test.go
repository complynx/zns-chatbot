package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestLocalRegistrationToolReadsHTTPParity(t *testing.T) {
	t.Parallel()
	db, service, local, remote := registrationOperationsFixture(t)
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "tool-reads-booking", passbooking.Booking{}))
	require.NoError(t, err)
	capabilities, err := local.PassToolCapabilities(t.Context(), "bob")
	require.NoError(t, err)
	require.True(t, capabilities.Export)
	remoteCapabilities, err := remote.PassToolCapabilities(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(t, remoteCapabilities, capabilities)
	tiers, err := local.PassTierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	remoteTiers, err := remote.PassTierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, remoteTiers, tiers)
	require.Equal(t, "dance", tiers.Event)
	for _, client := range []appclient.Client{local, remote} {
		_, readErr := client.PassTierStatus(t.Context(), "alice", "dance")
		requireCode(t, readErr, "forbidden")
		_, readErr = client.PassToolCapabilities(t.Context(), "missing-owner")
		requireCode(t, readErr, "forbidden")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.PassToolCapabilities(ctx, "bob")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassTierStatus(ctx, "bob", "dance")
		require.ErrorIs(t, readErr, context.Canceled)
	}
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins WHERE owner='bob'; INSERT INTO core.pass_events(id,finishes_at) VALUES('other',now()+interval '1 day')`,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		current, readErr := client.PassToolCapabilities(t.Context(), "bob")
		require.NoError(t, readErr)
		require.True(t, current.Export)
		_, readErr = client.PassTierStatus(t.Context(), "bob", "dance")
		require.NoError(t, readErr)
		_, readErr = client.PassTierStatus(t.Context(), "bob", "other")
		requireCode(t, readErr, "forbidden")
	}
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		current, readErr := client.PassToolCapabilities(t.Context(), "bob")
		require.NoError(t, readErr)
		require.False(t, current.Export)
		_, readErr = client.PassTierStatus(t.Context(), "bob", "dance")
		requireCode(t, readErr, "forbidden")
	}
}
