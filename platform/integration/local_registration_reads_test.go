package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestLocalRegistrationReadsHTTPParity(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	signer := identity.Signer{Key: []byte("local-registration-reads-test-key")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(api.AuthenticatedHandler(appservices.Services{
		Core: core.Service{DB: db}, Registration: service,
	}, signer, slog.New(slog.DiscardHandler), verify))
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{SandboxToken: signer.Token, LocalRegistration: &appclient.LocalRegistration{
		Service: service, Authorizer: applicationauth.Authorizer{DB: db, Verify: verify},
	}}
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
 SELECT 'read-inviter-'||n,10000+n,'Inviter '||n,true FROM generate_series(1,26) n;
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,invitation_target,created_at)
 SELECT 'dance','read-inviter-'||n,1,'waiting-for-couple','leader','couple','bob',202,
 '2026-01-01 00:00:00+00'::timestamptz FROM generate_series(1,26) n;`)
	require.NoError(t, err)
	events, err := local.PassEvents(t.Context(), "bob")
	require.NoError(t, err)
	remoteEvents, err := remote.PassEvents(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(t, remoteEvents, events)
	require.Len(t, events, 1)
	admins, err := local.PassPaymentAdmins(t.Context(), "bob", "dance")
	require.NoError(t, err)
	remoteAdmins, err := remote.PassPaymentAdmins(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, remoteAdmins, admins)
	require.Len(t, admins, 1)
	cursor := ""
	for _, count := range []int{25, 1} {
		invites, readErr := local.PassInvitations(t.Context(), "bob", "dance", cursor)
		require.NoError(t, readErr)
		remoteInvites, readErr := remote.PassInvitations(t.Context(), "bob", "dance", cursor)
		require.NoError(t, readErr)
		require.Equal(t, remoteInvites, invites)
		require.Len(t, invites.Invitations, count)
		queue, readErr := local.PassQueue(t.Context(), "bob", "dance", cursor)
		require.NoError(t, readErr)
		remoteQueue, readErr := remote.PassQueue(t.Context(), "bob", "dance", cursor)
		require.NoError(t, readErr)
		require.Equal(t, remoteQueue, queue)
		require.Len(t, queue.Bookings, count)
		require.Equal(t, invites.Next, queue.Next)
		cursor = invites.Next
		if count == 25 {
			require.NotEmpty(t, cursor)
		} else {
			require.Empty(t, cursor)
		}
	}
	for _, client := range []appclient.Client{local, remote} {
		private, readErr := client.PassInvitations(t.Context(), "alice", "dance", "")
		require.NoError(t, readErr)
		require.Empty(t, private.Invitations)
		for _, invalid := range []string{"invalid", strings.Repeat("x", 101)} {
			_, readErr = client.PassInvitations(t.Context(), "bob", "dance", invalid)
			requireCode(t, readErr, "pass_booking_invalid")
			_, readErr = client.PassQueue(t.Context(), "bob", "dance", invalid)
			requireCode(t, readErr, "pass_booking_invalid")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.PassEvents(ctx, "bob")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassInvitations(ctx, "bob", "dance", "")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassPaymentAdmins(ctx, "bob", "dance")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassQueue(ctx, "bob", "dance", "")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassEvents(t.Context(), "missing-owner")
		requireCode(t, readErr, "forbidden")
	}
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins WHERE owner='bob'; UPDATE core.pass_payment_admins SET hidden=true WHERE owner='bob'`,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		queue, readErr := client.PassQueue(t.Context(), "bob", "dance", "")
		requireCode(t, readErr, "forbidden")
		require.Empty(t, queue)
		current, readErr := client.PassPaymentAdmins(t.Context(), "bob", "dance")
		require.NoError(t, readErr)
		require.Empty(t, current)
	}
}
