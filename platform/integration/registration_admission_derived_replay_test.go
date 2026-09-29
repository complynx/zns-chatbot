package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

func TestRegistrationAdmissionDerivedCommittedReplayReconciles(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"valid", "revoked", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			ctx := t.Context()
			handle(t, f.b, message(78701, 101, "/passes"))
			handle(t, f.b, message(78702, 101, "/passes"))
			earlier := registrationingress.WithReference(
				ctx,
				registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78701},
			)
			later := registrationingress.WithReference(
				ctx,
				registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: 78702},
			)
			history := conversation.Service{DB: f.db}
			require.NoError(
				t,
				history.AppendOriginal(ctx, "alice", "admission-source", "user", "private source canary"),
			)
			source := registrationDerivation(t, f.db, "alice")
			command := bookingCommand("solo", "derived-same-key", passbooking.Booking{})
			first, err := f.b.Host.AdmitPassBooking(later, "alice", command, &source)
			require.NoError(t, err)
			booked, err := f.b.Host.ExecuteDerivedPassBooking(later, "alice", command, source)
			require.NoError(t, err)
			switch mode {
			case "revoked":
				_, err = f.db.Exec(ctx, "DELETE FROM core.knowledge_permissions WHERE actor='alice'")
				require.NoError(t, err)
			case "deleted":
				require.NoError(
					t,
					history.DeleteContent(ctx, "alice", lazyHistoryID(t, history, "alice", "admission-source")),
				)
			}
			replayed, err := f.b.Host.ExecuteDerivedPassBooking(earlier, "alice", command, source)
			if mode == "valid" {
				require.NoError(t, err)
				require.Equal(t, booked, replayed, "the committed booking must not run again")
			} else {
				require.Error(t, err)
				require.Empty(t, replayed, "rejected source must not disclose the committed booking")
			}
			var position, version int64
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT ingress_id FROM core.registration_intents WHERE id=$1", first.ID).
					Scan(&position),
			)
			if mode == "valid" {
				require.Less(t, position, first.Position)
			} else {
				require.Equal(t, first.Position, position)
			}
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT version FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'").
					Scan(&version),
			)
			require.Equal(t, booked.Version, version)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT count(*) FROM core.pass_booking_operations WHERE event_id='dance' AND actor='alice'").
					Scan(&count),
			)
			require.Equal(t, 1, count)
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intent_requests WHERE event_id='dance' AND owner='alice'").
					Scan(&count),
			)
			require.Equal(t, 1, count)
		})
	}
}
