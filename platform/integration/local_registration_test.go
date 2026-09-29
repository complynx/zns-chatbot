package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestLocalRegistrationHTTPParity(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	signer := identity.Signer{Key: []byte("local-registration-test-key-32-bytes")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(api.AuthenticatedHandler(appservices.Services{
		Core: core.Service{DB: db}, Registration: service,
	}, signer, slog.New(slog.DiscardHandler), verify))
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{SandboxToken: signer.Token, LocalRegistration: &appclient.LocalRegistration{
		Service: service, Authorizer: applicationauth.Authorizer{DB: db, Verify: verify},
	}}
	command := bookingCommand("solo", "local-registration", passbooking.Booking{})
	created, err := local.ExecutePassBooking(t.Context(), "alice", command)
	require.NoError(t, err)
	replayed, err := remote.ExecutePassBooking(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Equal(t, created, replayed)
	for _, client := range []appclient.Client{local, remote} {
		read, readErr := client.PassBooking(t.Context(), "alice", "dance")
		require.NoError(t, readErr)
		require.Equal(t, created, read)
		_, invalidErr := client.ExecutePassBooking(t.Context(), "alice", passbooking.Command{})
		requireCode(t, invalidErr, "pass_booking_invalid")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, canceledErr := client.PassBooking(ctx, "alice", "dance")
		require.ErrorIs(t, canceledErr, context.Canceled)
		_, canceledErr = client.ExecutePassBooking(ctx, "alice", command)
		require.ErrorIs(t, canceledErr, context.Canceled)
	}
	_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	command = bookingCommand("cancel", "permission-withdrawn", created)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.ExecutePassBooking(t.Context(), "alice", command)
		requireCode(t, err, "forbidden")
	}
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).Scan(&count),
	)
	require.Equal(t, 1, count)
}
