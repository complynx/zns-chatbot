package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestRegistrationAdmissionManualMissingProfileAndReplay(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	handlePassVisible(t, f, message(78001, 101, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 78002, "Dance"))
	var count int
	require.NoError(t, f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intents").Scan(&count))
	require.Zero(t, count, "navigation must not acquire event priority")
	click := passMenuClick(t, f, 101, 78003, "Register solo")
	_, err := f.db.Exec(ctx, "DELETE FROM core.pass_profiles WHERE owner='alice'")
	require.NoError(t, err)
	handlePassVisible(t, f, click)
	var intent, position int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id,ingress_id FROM core.registration_intents WHERE event_id='dance' AND owner='alice'`).
			Scan(&intent, &position),
	)
	var key, state string
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT request_key FROM core.registration_ingress WHERE id=$1`, position).Scan(&key),
	)
	require.Equal(t, "78003", key)
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT state FROM core.registration_intents WHERE id=$1`, intent).Scan(&state),
	)
	require.Equal(t, "captured", state)
	require.NoError(t, f.db.QueryRow(ctx, "SELECT count(*) FROM core.pass_bookings WHERE owner='alice'").Scan(&count))
	require.Zero(t, count)
	// A restart and a fresh form completion reuse the original intent.
	restarted := *f.b
	f.b = &restarted
	_, err = f.db.Exec(ctx, "INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader')")
	require.NoError(t, err)
	handlePassVisible(t, f, message(78004, 101, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 78005, "Dance"))
	handlePassVisible(t, f, passMenuClick(t, f, 101, 78006, "Register solo"))
	var after, afterPosition int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id,ingress_id FROM core.registration_intents WHERE event_id='dance' AND owner='alice'`).
			Scan(&after, &afterPosition),
	)
	require.Equal(t, intent, after)
	require.Equal(t, position, afterPosition)
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT state FROM core.registration_intents WHERE id=$1`, intent).Scan(&state),
	)
	require.Equal(t, "registered", state)
}

func TestRegistrationAdmissionPublicRoutePreopenAndCancelGeneration(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	ctx := t.Context()
	_, err := f.db.Exec(ctx, "UPDATE core.pass_event_tiers SET starts_at=now()+interval '1 day' WHERE event_id='dance'")
	require.NoError(t, err)
	command := bookingCommand("solo", "opening-attempt", passbooking.Booking{})
	_, err = f.b.API.ExecutePassBooking(ctx, "alice", command)
	require.Error(t, err)
	var id, generation int64
	var open bool
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id,generation,sales_open FROM core.registration_intents WHERE event_id='dance' AND owner='alice'`).
			Scan(&id, &generation, &open),
	)
	require.False(t, open)
	_, err = f.b.API.ExecutePassBooking(ctx, "alice", command)
	require.Error(t, err)
	_, err = f.db.Exec(ctx, "UPDATE core.pass_event_tiers SET starts_at=now()-interval '1 day' WHERE event_id='dance'")
	require.NoError(t, err)
	booking, err := f.b.API.ExecutePassBooking(ctx, "alice", command)
	require.NoError(t, err)
	cancel := bookingCommand("cancel", "cancel-first", booking)
	cancelled, err := f.b.API.ExecutePassBooking(ctx, "alice", cancel)
	require.NoError(t, err)
	next := bookingCommand("solo", "second-generation", cancelled)
	_, err = f.b.API.ExecutePassBooking(ctx, "alice", next)
	require.NoError(t, err)
	var nextID, nextGeneration int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id,generation FROM core.registration_intents WHERE event_id='dance' AND owner='alice' AND state<>'cancelled'`).
			Scan(&nextID, &nextGeneration),
	)
	require.NotEqual(t, id, nextID)
	require.Equal(t, generation+1, nextGeneration)
	_, err = f.b.API.ExecutePassBooking(ctx, "alice", command)
	require.NoError(t, err, "committed operation replay must not start another generation")
	var count int
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intents WHERE owner='alice'").Scan(&count),
	)
	require.Equal(t, 2, count)
}

func TestRegistrationAdmissionSavedDerivedUsesOriginalIngress(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	f.b.Delivery.BotID = 99078
	ctx := t.Context()
	const updateID int64 = 78101
	command := bookingCommand("solo", "saved-admission", passbooking.Booking{})
	plan := interaction.SavedPlan{
		Plan:                agent.Plan{View: agent.RegistrationView},
		RegistrationCommand: &command,
		PassAuthority: &interaction.PlanAuthority{
			Reads:           []interaction.PassContextDependency{},
			ReadAuthorities: []readsource.Authority{},
		},
	}
	plan.BindKind()
	_, err := (interaction.Store{DB: f.db}).SaveWinner(ctx, "alice", updateID, plan)
	require.NoError(t, err)
	require.NoError(t, f.b.Handle(ctx, message(updateID, identity.AliceTelegramID, "Register for Dance")))
	var requestKey string
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT r.request_key FROM core.registration_intents i JOIN core.registration_ingress r ON r.id=i.ingress_id WHERE i.owner='alice' AND i.event_id='dance'`).
			Scan(&requestKey),
	)
	require.Equal(t, "78101", requestKey)
	require.NoError(t, f.b.Handle(ctx, message(updateID, identity.AliceTelegramID, "Register for Dance")))
	var count int
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intents WHERE owner='alice'").Scan(&count),
	)
	require.Equal(t, 1, count)
	require.Zero(t, f.model.calls)
}
