package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func registrationOperationsFixture(
	t *testing.T,
) (*pgxpool.Pool, passbooking.Service, appclient.Client, appclient.Client) {
	t.Helper()
	db, service := bookingFixture(t)
	signer := identity.Signer{Key: []byte("registration-operations-test-key-32")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.Services{Core: core.Service{DB: db}, Registration: service},
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{
		SandboxToken: signer.Token,
		LocalRegistration: &appclient.LocalRegistration{
			Service:    service,
			Authorizer: applicationauth.Authorizer{DB: db, Verify: verify},
		},
	}
	return db, service, local, remote
}

func TestLocalRegistrationAdminHTTPParity(t *testing.T) {
	t.Parallel()
	db, service, local, remote := registrationOperationsFixture(t)
	booking, err := service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "admin-parity-register", passbooking.Booking{}),
	)
	require.NoError(t, err)
	parity := func(client appclient.Client) (passbooking.Capabilities, passbooking.AdminTarget, passbooking.TakeoverTarget) {
		caps, readErr := client.PassCapabilities(t.Context(), "bob", "dance")
		require.NoError(t, readErr)
		target, readErr := client.PassAdminTarget(t.Context(), "bob", "dance", 101)
		require.NoError(t, readErr)
		takeover, readErr := client.PassTakeoverTarget(t.Context(), "bob", "dance", 101)
		require.NoError(t, readErr)
		return caps, target, takeover
	}
	caps, target, takeover := parity(local)
	remoteCaps, remoteTarget, remoteTakeover := parity(remote)
	require.Equal(t, remoteCaps, caps)
	require.Equal(t, remoteTarget, target)
	require.Equal(t, remoteTakeover, takeover)
	price := 0
	command := passbooking.AdminAssignment{
		Event:         "dance",
		Key:           "local-admin-assignment",
		Target:        "alice",
		TargetVersion: booking.Version,
		TotalPrice:    &price,
	}
	assigned, err := local.AssignPass(t.Context(), "bob", command)
	require.NoError(t, err)
	replay, err := remote.AssignPass(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Equal(t, assigned, replay)
	require.Len(t, assigned.Bookings, 1)
	require.Equal(t, "paid", assigned.Bookings[0].State)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.PassAdminTarget(t.Context(), "bob", "dance", 0)
		requireCode(t, err, "invalid_json")
		_, err = client.PassTakeoverTarget(t.Context(), "bob", "dance", 0)
		requireCode(t, err, "pass_booking_invalid")
		invalid := command
		invalid.Key = strings.Repeat("x", 201)
		_, err = client.AssignPass(t.Context(), "bob", invalid)
		requireCode(t, err, "pass_booking_invalid")
		_, err = client.AssignPass(t.Context(), "alice", command)
		requireCode(t, err, "forbidden")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = client.AssignPass(ctx, "bob", command)
		require.ErrorIs(t, err, context.Canceled)
		_, err = client.PassCapabilities(ctx, "bob", "dance")
		require.ErrorIs(t, err, context.Canceled)
		_, err = client.PassAdminTarget(ctx, "bob", "dance", 101)
		require.ErrorIs(t, err, context.Canceled)
		_, err = client.PassTakeoverTarget(ctx, "bob", "dance", 101)
		require.ErrorIs(t, err, context.Canceled)
	}
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins WHERE owner='bob'; DELETE FROM core.pass_payment_admins WHERE owner='bob'`,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.AssignPass(t.Context(), "bob", command)
		requireCode(t, err, "forbidden")
		_, err = client.PassAdminTarget(t.Context(), "bob", "dance", 101)
		requireCode(t, err, "forbidden")
		_, err = client.PassTakeoverTarget(t.Context(), "bob", "dance", 101)
		requireCode(t, err, "forbidden")
	}
}

func TestLocalRegistrationPaymentHTTPParity(t *testing.T) {
	t.Parallel()
	db, service, local, remote := registrationOperationsFixture(t)
	booking, err := service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "payment-parity-register", passbooking.Booking{}),
	)
	require.NoError(t, err)
	quote, err := local.PassPaymentQuote(t.Context(), "alice", "dance")
	require.NoError(t, err)
	remoteQuote, err := remote.PassPaymentQuote(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, remoteQuote, quote)
	require.Positive(t, quote.Total)
	proof, err := (orders.Service{DB: db}).UploadProof(
		t.Context(),
		"alice",
		"synthetic.txt",
		[]byte("synthetic receipt"),
	)
	require.NoError(t, err)
	submit := bookingCommand("proof", "operations-proof", booking)
	submit.ProofID = proof.ID
	_, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_payment_admins SET hidden=true WHERE owner='bob'; INSERT INTO core.pass_booking_admins(owner) VALUES('visitor')`,
	)
	require.NoError(t, err)
	payment, err := local.PassPayment(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	remotePayment, err := remote.PassPayment(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, remotePayment, payment)
	queue, err := local.PassPaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	remoteQueue, err := remote.PassPaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Equal(t, remoteQueue, queue)
	require.Len(t, queue.Items, 1)
	for _, client := range []appclient.Client{local, remote} {
		own, readErr := client.PassPayment(t.Context(), "alice", "dance", "alice")
		require.NoError(t, readErr)
		require.Equal(t, payment, own)
		_, readErr = client.PassPayment(t.Context(), "visitor", "dance", "alice")
		requireCode(t, readErr, "forbidden")
		_, readErr = client.PassPaymentQueue(t.Context(), "visitor", "dance", "")
		requireCode(t, readErr, "forbidden")
		_, readErr = client.PassPaymentQueue(t.Context(), "bob", "dance", "bad")
		requireCode(t, readErr, "pass_booking_invalid")
		page, readErr := client.PassPaymentQueue(t.Context(), "bob", "dance", payment.Attempt)
		require.NoError(t, readErr)
		require.Empty(t, page.Items)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.PassPayment(ctx, "bob", "dance", "alice")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassPaymentQuote(ctx, "alice", "dance")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassPaymentQueue(ctx, "bob", "dance", "")
		require.ErrorIs(t, readErr, context.Canceled)
	}
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.PassPayment(t.Context(), "bob", "dance", "alice")
		requireCode(t, err, "forbidden")
		_, err = client.PassPaymentQueue(t.Context(), "bob", "dance", "")
		requireCode(t, err, "forbidden")
	}
}
