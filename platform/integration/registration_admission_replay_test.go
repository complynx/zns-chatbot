package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

func TestRegistrationAdmissionEarlierDuplicateReconciles(t *testing.T) {
	t.Parallel()
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "captured", true: "committed"}[committed], func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			ctx := t.Context()
			handle(t, f.b, message(78401, 101, "/passes"))
			handle(t, f.b, message(78402, 202, "/passes"))
			handle(t, f.b, message(78403, 101, "/passes"))
			command := bookingCommand("solo", "same-button-key", passbooking.Booking{})
			earlier := registrationingress.WithReference(
				ctx,
				registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78401},
			)
			later := registrationingress.WithReference(
				ctx,
				registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78403},
			)
			first, err := f.b.Host.AdmitPassBooking(later, "alice", command, nil)
			require.NoError(t, err)
			if committed {
				_, err = f.b.API.ExecutePassBooking(ctx, "alice", command)
				require.NoError(t, err)
			}
			corrected, err := f.b.Host.AdmitPassBooking(earlier, "alice", command, nil)
			require.NoError(t, err)
			require.Equal(t, first.ID, corrected.ID)
			require.Less(t, corrected.Position, first.Position)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intent_requests WHERE owner='alice'").
					Scan(&count),
			)
			require.Equal(t, 1, count)
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_ingress WHERE kind='application'").
					Scan(&count),
			)
			require.Zero(t, count, "ordinary replay must not manufacture an application ingress")
			foreign := registrationingress.WithReference(
				ctx,
				registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78402},
			)
			_, err = f.b.Host.AdmitPassBooking(foreign, "alice", command, nil)
			require.Error(t, err)
			again, err := f.b.Host.AdmitPassBooking(later, "alice", command, nil)
			require.NoError(t, err)
			require.Equal(t, corrected.Position, again.Position)
		})
	}
}

func TestRegistrationAdmissionStaleEarlierRequestOnlyChangesEvidence(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	handle(t, f.b, message(78501, 101, "/passes"))
	handle(t, f.b, message(78502, 101, "/passes"))
	earlier := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78501},
	)
	later := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78502},
	)
	command := bookingCommand("solo", "later-booking", passbooking.Booking{})
	first, err := f.b.Host.AdmitPassBooking(later, "alice", command, nil)
	require.NoError(t, err)
	booked, err := f.b.API.ExecutePassBooking(ctx, "alice", command)
	require.NoError(t, err)
	command.Key = "earlier-stale-button"
	corrected, err := f.b.Host.AdmitPassBooking(earlier, "alice", command, nil)
	require.NoError(t, err)
	require.Equal(t, first.ID, corrected.ID)
	require.Less(t, corrected.Position, first.Position)
	_, err = f.b.API.ExecutePassBooking(ctx, "alice", command)
	require.Error(t, err, "capturing evidence must not accept the stale domain effect")
	var version int64
	require.NoError(t, f.db.QueryRow(ctx, "SELECT version FROM core.pass_bookings WHERE owner='alice'").Scan(&version))
	require.Equal(t, booked.Version, version)
}

func TestRegistrationAdmissionReplayCannotCrossCancelledGeneration(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	handle(t, f.b, message(78601, 101, "/passes"))
	handle(t, f.b, message(78602, 101, "/passes"))
	earlier := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78601},
	)
	later := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78602},
	)
	command := bookingCommand("solo", "cancelled-button", passbooking.Booking{})
	first, err := f.b.Host.AdmitPassBooking(later, "alice", command, nil)
	require.NoError(t, err)
	_, err = f.b.API.ExecutePassBooking(ctx, "alice", bookingCommand("cancel", "close-capture", passbooking.Booking{}))
	require.NoError(t, err)
	retired, err := f.b.Host.AdmitPassBooking(earlier, "alice", command, nil)
	require.NoError(t, err)
	require.Equal(t, first.Position, retired.Position)
	require.Equal(t, "cancelled", retired.State)
	handle(t, f.b, message(78603, 101, "/passes"))
	fresh := registrationingress.WithReference(
		ctx,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78603},
	)
	command.Key = "new-generation"
	next, err := f.b.Host.AdmitPassBooking(fresh, "alice", command, nil)
	require.NoError(t, err)
	_, err = f.b.Host.AdmitPassBooking(earlier, "alice", command, nil)
	require.Error(t, err, "same-key replay must not move a new generation across its cancellation frontier")
	var position int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT ingress_id FROM core.registration_intents WHERE id=$1", next.ID).Scan(&position),
	)
	require.Equal(t, next.Position, position)
}
