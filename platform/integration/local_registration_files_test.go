package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestLocalRegistrationFilesHTTPParity(t *testing.T) {
	t.Parallel()
	db, registration := bookingFixture(t)
	files := orders.Service{DB: db}
	signer := identity.Signer{Key: []byte("registration-files-test-key-32bytes")}
	verify := func(_ context.Context, token string) (string, error) { return signer.Verify(token) }
	authorizer := applicationauth.Authorizer{DB: db, Verify: verify}
	server := httptest.NewServer(
		api.AuthenticatedHandler(
			appservices.Services{Core: core.Service{DB: db}, Registration: registration, Orders: files},
			signer,
			slog.New(slog.DiscardHandler),
			verify,
		),
	)
	t.Cleanup(server.Close)
	remote := appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
	local := appclient.Client{
		SandboxToken:      signer.Token,
		LocalRegistration: &appclient.LocalRegistration{Service: registration, Files: files, Authorizer: authorizer},
	}
	localHost := appclient.Host{
		UserToken: local.UserToken,
		LocalDerived: &appclient.LocalDerived{
			Service:    derivedmutation.Service{Registration: registration},
			Authorizer: authorizer,
		},
	}
	remoteHost := appclient.Host{Base: server.URL, HTTP: server.Client(), Signer: signer, UserToken: remote.UserToken}
	body := []byte("synthetic receipt bytes")
	proof, err := local.UploadPassProof(t.Context(), "alice", "чек.txt", body)
	require.NoError(t, err)
	replay, err := remote.UploadPassProof(t.Context(), "alice", "чек.txt", body)
	require.NoError(t, err)
	require.Equal(t, proof, replay)
	booking, err := local.ExecutePassBooking(
		t.Context(),
		"alice",
		bookingCommand("solo", "binary-register", passbooking.Booking{}),
	)
	require.NoError(t, err)
	command := bookingCommand("proof", "binary-proof", booking)
	command.ProofID = proof.ID
	paid, err := local.ExecutePassBooking(t.Context(), "alice", command)
	require.NoError(t, err)
	download, err := local.DownloadPassProof(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	remoteDownload, err := remote.DownloadPassProof(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, remoteDownload, download)
	require.Equal(t, body, download.Body)
	require.Equal(t, "чек.txt", download.Filename)
	require.Equal(t, paid.Version, download.Version)
	require.NotEmpty(t, download.Attempt)
	require.Empty(t, download.ID)
	tooLarge := make([]byte, orders.MaxProofBytes+1)
	for _, client := range []appclient.Client{local, remote} {
		_, readErr := client.UploadPassProof(t.Context(), "alice", "large.txt", tooLarge)
		var problem *core.ProblemError
		require.ErrorAs(t, readErr, &problem)
		require.Equal(t, http.StatusRequestEntityTooLarge, problem.Status)
		require.Equal(t, "invalid_proof", problem.Code)
		_, readErr = client.UploadPassProof(t.Context(), "alice", "../invalid", body)
		requireCode(t, readErr, "invalid_proof")
		_, readErr = client.DownloadPassProof(t.Context(), "visitor", "dance", "alice")
		requireCode(t, readErr, "forbidden")
		_, readErr = client.ExportPasses(t.Context(), "alice")
		requireCode(t, readErr, "forbidden")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.UploadPassProof(ctx, "alice", "check.txt", body)
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.DownloadPassProof(ctx, "bob", "dance", "alice")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.ExportPasses(ctx, "bob")
		require.ErrorIs(t, readErr, context.Canceled)
	}
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins WHERE owner='bob'; INSERT INTO core.pass_events(id,finishes_at) VALUES('other',now()+interval '2 days'); INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('other','bob')`,
	)
	require.NoError(t, err)
	export, err := local.ExportPasses(t.Context(), "bob")
	require.NoError(t, err)
	otherExport, err := remote.ExportPasses(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(
		t,
		exportRows(t, openExport(t, otherExport), "Passes"),
		exportRows(t, openExport(t, export), "Passes"),
	)
	snapshot, err := localHost.ExportPassSnapshot(t.Context(), "bob")
	require.NoError(t, err)
	otherSnapshot, err := remoteHost.ExportPassSnapshot(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(t, []string{"dance", "other"}, snapshot.Events)
	require.Equal(t, snapshot.Events, otherSnapshot.Events)
	require.Equal(
		t,
		exportRows(t, openExport(t, snapshot.Body), "Passes"),
		exportRows(t, openExport(t, otherSnapshot.Body), "Passes"),
	)
	for _, host := range []appclient.Host{localHost, remoteHost} {
		require.NoError(t, host.CheckPassExportSnapshot(t.Context(), "bob", snapshot.Events))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = host.ExportPassSnapshot(ctx, "bob")
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, host.CheckPassExportSnapshot(ctx, "bob", snapshot.Events), context.Canceled)
	}
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.pass_payment_admins WHERE owner='bob' AND event_id='dance'; UPDATE core.users SET can_book=false WHERE id='alice'`,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.UploadPassProof(t.Context(), "alice", "чек.txt", body)
		requireCode(t, err, "forbidden")
		_, err = client.DownloadPassProof(t.Context(), "bob", "dance", "alice")
		requireCode(t, err, "forbidden")
		_, err = client.ExportPasses(t.Context(), "bob")
		require.NoError(t, err)
	}
	for _, host := range []appclient.Host{localHost, remoteHost} {
		requireCode(t, host.CheckPassExportSnapshot(t.Context(), "bob", snapshot.Events), "forbidden")
	}
}
