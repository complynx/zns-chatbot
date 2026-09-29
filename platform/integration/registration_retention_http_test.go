package integration_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRegistrationRetentionHTTPIntakePrecedesAllocationLock(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, "DELETE FROM core.pass_profiles WHERE owner='alice'")
	require.NoError(t, err)
	services := notificationFixtureServices(db, appservices.Options{})
	services.Registration = service
	signer := identity.Signer{Key: []byte(strings.Repeat("i", 32))}
	server := httptest.NewServer(api.Handler(services, signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	client := appclient.Client{Base: server.URL, SandboxToken: signer.Token}
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(ctx)) })
	_, err = blocker.Exec(ctx, "SELECT id FROM core.pass_events WHERE id='dance' FOR NO KEY UPDATE")
	require.NoError(t, err)
	aliceDone := make(chan error, 1)
	go func() {
		_, callErr := client.ExecutePassBooking(
			ctx,
			"alice",
			bookingCommand("solo", "http-earlier", passbooking.Booking{}),
		)
		aliceDone <- callErr
	}()
	require.Eventually(t, func() bool {
		var count int
		readErr := db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intents WHERE event_id='dance' AND owner='alice'").
			Scan(&count)
		return readErr == nil && count == 1
	}, 5*time.Second, 10*time.Millisecond, "accepted HTTP intake must commit before waiting for event allocation")
	first := readRegistrationTurn(t, db, "alice")
	type bookingResult struct {
		booking passbooking.Booking
		err     error
	}
	bobDone := make(chan bookingResult, 1)
	go func() {
		value, callErr := client.ExecutePassBooking(
			ctx,
			"bob",
			bookingCommand("solo", "http-later", passbooking.Booking{}),
		)
		bobDone <- bookingResult{value, callErr}
	}()
	require.Eventually(t, func() bool {
		var count int
		readErr := db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intents WHERE event_id='dance' AND owner='bob'").
			Scan(&count)
		return readErr == nil && count == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Less(t, first.Position, readRegistrationTurn(t, db, "bob").Position)
	require.NoError(t, blocker.Rollback(ctx))
	select {
	case callErr := <-aliceDone:
		requireCode(t, callErr, "pass_profile_required")
	case <-time.After(5 * time.Second):
		t.Fatal("earlier HTTP request did not complete")
	}
	select {
	case result := <-bobDone:
		require.NoError(t, result.err)
		require.Equal(t, "waitlist", result.booking.State)
	case <-time.After(5 * time.Second):
		t.Fatal("later HTTP request did not complete")
	}
	require.Equal(t, first, readRegistrationTurn(t, db, "alice"))
	expireAliceRegistrationTurn(t, db)
	restarted := passbooking.Service{DB: db, Delivery: service.Delivery}
	_, err = restarted.ProcessDeadlines(ctx)
	require.NoError(t, err)
	require.Equal(t, first.ID, readRegistrationTurn(t, db, "alice").ID)
	bob, err := restarted.Get(ctx, "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, "assigned", bob.State)
}
