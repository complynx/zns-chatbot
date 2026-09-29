package integration_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

func TestRegistrationAdmissionIngressSurvivesInboxRemoval(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	f.b.Delivery.BotID = 99078
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "/passes"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 202, "text": "/passes"})
	completeInbox(t, f, 3)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.registration_ingress WHERE kind='telegram' AND bot_id=99078").
			Scan(&count),
	)
	require.Equal(t, 2, count)
	require.NoError(t, f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.registration_intents").Scan(&count))
	require.Zero(t, count)
	var first, second int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT id FROM core.registration_ingress WHERE request_key='1'").Scan(&first),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT id FROM core.registration_ingress WHERE request_key='2'").Scan(&second),
	)
	require.Less(t, first, second)
}

func TestRegistrationAdmissionRejectsForeignIngressAndRetiredSource(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	f.b.Delivery.BotID = 99078
	handle(t, f.b, message(78201, 202, "/passes"))
	handle(t, f.b, message(78202, 101, "/passes"))
	command := bookingCommand("solo", "foreign-ingress", passbooking.Booking{})
	foreign := registrationingress.WithReference(
		t.Context(),
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78201},
	)
	_, err := f.b.Host.AdmitPassBooking(foreign, "alice", command, nil)
	require.Error(t, err)
	own := registrationingress.WithReference(
		t.Context(),
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78202},
	)
	var staleGeneration int64 = 9000
	source := readsource.Derivation{Generation: &staleGeneration, Authorities: []readsource.Authority{}}
	_, err = f.b.Host.AdmitPassBooking(own, "alice", command, &source)
	require.Error(t, err)
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.registration_intents").Scan(&count))
	require.Zero(t, count)
	_, err = f.db.Exec(t.Context(), "UPDATE core.users SET can_book=false WHERE id='alice'")
	require.NoError(t, err)
	_, err = f.b.Host.AdmitPassBooking(own, "alice", command, nil)
	require.Error(t, err)
	require.NoError(t, f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.registration_intents").Scan(&count))
	require.Zero(t, count)
}

func TestRegistrationAdmissionConcurrentRequestsReuseIntent(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), "DELETE FROM core.pass_profiles WHERE owner='alice'")
	require.NoError(t, err)
	const requests = 8
	var group sync.WaitGroup
	failures := make(chan error, requests)
	for i := range requests {
		group.Go(func() {
			command := bookingCommand("solo", string(rune('a'+i)), passbooking.Booking{})
			_, callErr := f.b.API.ExecutePassBooking(t.Context(), "alice", command)
			failures <- callErr
		})
	}
	group.Wait()
	close(failures)
	for callErr := range failures {
		require.Error(t, callErr)
	}
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.registration_intents WHERE owner='alice'").Scan(&count),
	)
	require.Equal(t, 1, count)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.registration_intent_requests WHERE owner='alice'").
			Scan(&count),
	)
	require.Equal(t, requests, count)
}

func TestRegistrationAdmissionUnfinishedCancellationFencesOlderRequests(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	for _, id := range []int64{78301, 78302, 78303} {
		handle(t, f.b, message(id, 101, "/passes"))
	}
	command := bookingCommand("solo", "unfinished-first", passbooking.Booking{})
	later := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78302},
	)
	first, err := f.b.Host.AdmitPassBooking(later, "alice", command, nil)
	require.NoError(t, err)
	earlier := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78301},
	)
	command.Key = "earlier-completed-late"
	corrected, err := f.b.Host.AdmitPassBooking(earlier, "alice", command, nil)
	require.NoError(t, err)
	require.Equal(t, first.ID, corrected.ID)
	require.Less(t, corrected.Position, first.Position)
	_, err = f.b.API.ExecutePassBooking(
		ctx,
		"alice",
		bookingCommand("cancel", "cancel-unfinished", passbooking.Booking{}),
	)
	require.NoError(t, err)
	old := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78303},
	)
	command.Key = "delayed-before-cancel"
	_, err = f.b.Host.AdmitPassBooking(old, "alice", command, nil)
	require.Error(t, err, "an older unresolved request cannot revive the cancelled generation")
	handle(t, f.b, message(78304, 101, "/passes"))
	fresh := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78304},
	)
	command.Key = "explicit-new-generation"
	next, err := f.b.Host.AdmitPassBooking(fresh, "alice", command, nil)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, next.ID)
	require.Equal(t, first.Generation+1, next.Generation)
	var count int
	require.NoError(t, f.db.QueryRow(ctx, "SELECT count(*) FROM core.pass_bookings WHERE owner='alice'").Scan(&count))
	require.Zero(t, count)
}
