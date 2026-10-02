package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestRegistrationAdmissionLegacyMigrationPreservesUnknownIngress(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "legacy-alice", passbooking.Booking{}))
	require.NoError(t, err)
	bob, err := service.Execute(t.Context(), "bob", bookingCommand("solo", "legacy-bob", passbooking.Booking{}))
	require.NoError(t, err)
	bob, err = service.Execute(t.Context(), "bob", bookingCommand("cancel", "legacy-cancel", bob))
	require.NoError(t, err)

	// Reconstruct the pre-078 schema only in this test's private database.
	_, err = db.Exec(t.Context(), `DROP TABLE core.registration_intent_requests;
 DROP TABLE core.registration_intents;
 DROP TABLE core.registration_ingress; DROP FUNCTION core.registration_native_immutable();
 DELETE FROM public.zns_schema_migrations WHERE name IN ('078_registration_admission.sql','082_registration_retention.sql','083_registration_native_intake.sql','091_admin_page_ingress_proof.sql')`)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), db))
	for _, booking := range []passbooking.Booking{alice, bob} {
		var origin, state string
		var ingress *int64
		var salesOpen *bool
		var generation int64
		var sameCreated bool
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT origin,state,ingress_id,sales_open,generation,booking_created_at=$2
 FROM core.registration_intents WHERE event_id='dance' AND owner=$1`, booking.Owner, booking.CreatedAt).
				Scan(&origin, &state, &ingress, &salesOpen, &generation, &sameCreated),
		)
		require.Equal(t, "legacy_fallback", origin)
		require.Nil(t, ingress)
		require.Nil(t, salesOpen)
		require.EqualValues(t, 1, generation)
		require.True(t, sameCreated)
		if booking.State == "cancelled" {
			require.Equal(t, "cancelled", state)
		} else {
			require.Equal(t, "registered", state)
		}
	}
	_, err = service.Execute(t.Context(), "bob", bookingCommand("solo", "fresh-after-upgrade", bob))
	require.NoError(t, err)
	var canonical bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT origin='canonical_ingress' AND ingress_id IS NOT NULL AND sales_open IS NOT NULL
 FROM core.registration_intents WHERE owner='bob' AND event_id='dance' AND generation=2`).
			Scan(&canonical),
	)
	require.True(t, canonical)
}
