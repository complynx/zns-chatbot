package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestDerivedAdmissionDatabaseCommitFailure(t *testing.T) {
	t.Parallel()
	db, registration := bookingFixture(t)
	source := registrationDerivation(t, db, "alice")
	fault := &renderReadFailure{query: "commit"}
	config := db.Config()
	config.ConnConfig.Tracer = fault
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	request := passbooking.AdmissionRequest{
		Command: bookingCommand("solo", "admission-recovery", passbooking.Booking{}),
	}
	service := derivedmutation.Service{DB: broken, Registration: registration}
	_, err = service.CapturePassAdmission(t.Context(), "alice", request, source)
	require.True(t, fault.fired)
	require.NoError(t, fault.closeErr)
	require.Equal(t, core.ErrDatabase, err)
	var intents, receipts int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.registration_intents WHERE event_id='dance' AND owner='alice'),
 (SELECT count(*) FROM core.registration_intent_requests WHERE event_id='dance' AND owner='alice')`).Scan(&intents, &receipts))
	require.Zero(t, intents)
	require.Zero(t, receipts)
	service.DB = db
	result, err := service.CapturePassAdmission(t.Context(), "alice", request, source)
	require.NoError(t, err)
	require.Positive(t, result.ID)
	replay, err := service.CapturePassAdmission(t.Context(), "alice", request, source)
	require.NoError(t, err)
	require.Equal(t, result.ID, replay.ID)
	require.Equal(t, result.Generation, replay.Generation)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.registration_intents WHERE event_id='dance' AND owner='alice'),
 (SELECT count(*) FROM core.registration_intent_requests WHERE event_id='dance' AND owner='alice')`).Scan(&intents, &receipts))
	require.Equal(t, 1, intents)
	require.Equal(t, 1, receipts)
}
