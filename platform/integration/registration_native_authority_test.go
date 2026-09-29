package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func nativeIntakeHTTPClient(
	t *testing.T,
	f *fixture,
	authorize derivedmutation.NativeRegistrationAuthorizer,
) appclient.Client {
	t.Helper()
	services := notificationFixtureServices(f.db, appservices.Options{NativeRegistrationAuthorizer: authorize})
	signer := identity.Signer{Key: []byte(strings.Repeat("r", 32))}
	server := httptest.NewServer(api.Handler(services, signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	return appclient.Client{Base: server.URL, SandboxToken: signer.Token}
}

func TestRegistrationRetentionNativeAuthorityOutageIsEventScoped(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	stop := pauseNativeRegistration(t, f)
	defer stop()
	ctx := t.Context()
	var outage atomic.Bool
	outage.Store(true)
	authorize := fixtureNativeRegistrationAuthorizer(f.db)
	client := nativeIntakeHTTPClient(t, f, func(ctx context.Context, owner string, botID, sender int64) (bool, error) {
		if owner == "alice" && outage.Load() {
			return false, errors.New("synthetic identity outage")
		}
		return authorize(ctx, owner, botID, sender)
	})
	later := bookingCommand("solo", "outage-http", passbooking.Booking{})
	_, err := client.ExecutePassBooking(ctx, "bob", later)
	require.Error(t, err)
	var pending, count int
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_ingress WHERE native_event='dance' AND native_outcome=''").
			Scan(&pending),
	)
	require.Equal(t, 1, pending, "unknown identity must retain the original evidence")
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM core.pass_bookings WHERE event_id='dance'").Scan(&count),
	)
	require.Zero(t, count, "no slot may escape around unresolved earlier intake")
	laterTurn := readRegistrationTurn(t, f.db, "bob")
	_, err = f.db.Exec(
		ctx,
		`INSERT INTO core.pass_events(id,finishes_at,titles) VALUES('other',now()+interval '30 days','{"en":"Other"}');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('other',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('other','bob')`,
	)
	require.NoError(t, err)
	independent := bookingCommand("solo", "independent-http", passbooking.Booking{})
	independent.Event = "other"
	_, err = client.ExecutePassBooking(ctx, "bob", independent)
	require.NoError(t, err, "provider outage for one event must not stop independent event intake")
	outage.Store(false)
	booking, err := client.ExecutePassBooking(ctx, "bob", later)
	require.NoError(t, err)
	require.Equal(t, "waitlist", booking.State)
	require.Equal(t, laterTurn.Position, readRegistrationTurn(t, f.db, "bob").Position)
	require.Equal(t, laterTurn.Deadline, readRegistrationTurn(t, f.db, "bob").Deadline)
	require.Less(t, readRegistrationTurn(t, f.db, "alice").Position, laterTurn.Position)
}

func TestRegistrationRetentionRejectedNativeKeepsAnotherDraft(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	stop := pauseNativeRegistration(t, f)
	defer stop()
	ctx := t.Context()
	service := passbooking.Service{DB: f.db, Delivery: f.b.Delivery}
	command := bookingCommand("solo", "independent-alice-draft", passbooking.Booking{})
	_, err := service.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{Command: command})
	require.NoError(t, err)
	original := readRegistrationTurn(t, f.db, "alice")
	client := nativeIntakeHTTPClient(
		t,
		f,
		func(context.Context, string, int64, int64) (bool, error) { return false, nil },
	)
	_, err = client.ExecutePassBooking(ctx, "bob", bookingCommand("solo", "after-denied-native", passbooking.Booking{}))
	require.NoError(t, err)
	require.Equal(
		t,
		original,
		readRegistrationTurn(t, f.db, "alice"),
		"invalid evidence must not cancel or reprioritize another live draft",
	)
	var outcome string
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT native_outcome FROM core.registration_ingress WHERE native_event='dance'").
			Scan(&outcome),
	)
	require.Equal(t, "rejected", outcome)
	// Restoring current rights does not revive rejected ingress or steal old rank.
	restored := notificationFixtureServices(f.db, appservices.Options{})
	require.NoError(t, restored.Registration.ResolveRegistrationIntake(ctx, "dance"))
	require.Equal(t, original, readRegistrationTurn(t, f.db, "alice"))
	var savedCommand []byte
	var updateID int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT native_payload->'command',request_key::bigint FROM core.registration_ingress WHERE native_event='dance'").
			Scan(&savedCommand, &updateID),
	)
	var rejected passbooking.Command
	require.NoError(t, json.Unmarshal(savedCommand, &rejected))
	_, err = service.CaptureAdmission(
		ctx,
		"alice",
		passbooking.AdmissionRequest{
			Command: rejected,
			Ingress: &registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: updateID},
		},
	)
	requireCode(t, err, "pass_admission_rejected")
	require.Equal(t, original, readRegistrationTurn(t, f.db, "alice"))
}
