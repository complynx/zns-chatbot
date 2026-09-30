package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

// This test owns the one explicitly allocated database. It never runs against
// the general integration database, a FQA stand, or production.
func TestRegistrationFixtureRealStateAndRevocation(t *testing.T) {
	t.Parallel()
	db := registrationFixtureDatabase(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, `DROP SCHEMA IF EXISTS core,bot,interaction,credits CASCADE;
 DROP TABLE IF EXISTS public.zns_schema_migrations,public.zns_sandbox_fixtures`)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(ctx, db))
	require.NoError(t, store.Seed(ctx, db))
	read := sandbox.RegistrationFixture{Stand: sandbox.RegistrationFixtureStand, Action: "read"}
	_, err = sandbox.ApplyRegistrationFixture(ctx, db, read)
	require.ErrorContains(t, err, "not initialized")
	_, err = db.Exec(ctx, `UPDATE core.users SET telegram_id=909 WHERE id='alice'`)
	require.NoError(t, err)
	_, err = sandbox.ApplyRegistrationFixture(ctx, db, read)
	require.ErrorContains(t, err, "identity guard")
	_, err = db.Exec(ctx, `UPDATE core.users SET telegram_id=101 WHERE id='alice'`)
	require.NoError(t, err)
	require.NoError(t, sandbox.ApplyProductFixture(ctx, db))
	// A prior role change is not repaired by first initialization.
	_, err = db.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = sandbox.ApplyRegistrationFixture(ctx, db, sandbox.RegistrationFixture{
		Stand: sandbox.RegistrationFixtureStand, Action: "init", OpensAt: time.Now(),
	})
	require.Error(t, err)
	var admins int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.pass_booking_admins`).Scan(&admins))
	assert.Zero(t, admins)
	_, err = db.Exec(ctx, `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	f := sandbox.RegistrationFixture{
		Stand:   sandbox.RegistrationFixtureStand,
		Action:  "init",
		OpensAt: time.Now().Add(5 * time.Second),
	}
	state, err := sandbox.ApplyRegistrationFixture(ctx, db, f)
	require.NoError(t, err)
	require.Len(t, state.Rows, 6)
	service := passbooking.Service{DB: db}
	assertRegistrationFixtureRights(t, service)
	for _, row := range state.Rows {
		assert.Nil(t, row.Version)
		assert.Nil(t, row.IntentID)
	}

	// A real captured turn can be cancelled without fabricating a booking.
	admission, err := service.CaptureAdmission(ctx, "bob", passbooking.AdmissionRequest{Command: passbooking.Command{
		Name: "solo", Event: sandbox.RegistrationFixtureEventA, Key: "fixture-before-opening"}})
	require.NoError(t, err)
	require.NotNil(t, admission.SalesOpen)
	assert.False(t, *admission.SalesOpen)
	cancel := passbooking.Command{Name: "cancel", Event: sandbox.RegistrationFixtureEventA, Key: "fixture-cancel"}
	result, err := service.Execute(ctx, "bob", cancel)
	require.NoError(t, err)
	assert.Zero(t, result.Version)
	_, err = service.Execute(ctx, "bob", cancel)
	require.NoError(t, err)
	f.Action, f.OpensAt = "read", time.Time{}
	state, err = sandbox.ApplyRegistrationFixture(ctx, db, f)
	require.NoError(t, err)
	for _, row := range state.Rows {
		if row.Event == sandbox.RegistrationFixtureEventA && row.Owner == "bob" {
			require.NotNil(t, row.IntentState)
			assert.Equal(t, "cancelled", *row.IntentState)
			assert.Nil(t, row.Version)
			require.NotNil(t, row.IngressID)
			require.NotNil(t, row.ReceivedAt)
		}
	}

	// Use elapsed wall-clock opening and normal profile/registration operations.
	require.Eventually(t, func() bool {
		var open bool
		readErr := db.QueryRow(ctx, `SELECT starts_at<=clock_timestamp() FROM core.pass_event_tiers WHERE event_id=$1 AND position=0`, sandbox.RegistrationFixtureEventA).
			Scan(&open)
		return readErr == nil && open
	}, 15*time.Second, 10*time.Millisecond)
	profile := passes.Service{DB: db}
	for actor, role := range map[string]string{"alice": "leader", "bob": "follower"} {
		p, profileErr := profile.Execute(
			ctx,
			actor,
			passes.Command{Name: "set", Field: "role", Value: role, Origin: "manual", Key: "fixture-role"},
		)
		require.NoError(t, profileErr)
		_, profileErr = profile.Execute(
			ctx,
			actor,
			passes.Command{
				Name:    "set",
				Field:   "legal_name",
				Value:   "Private fixture name",
				Version: p.Version,
				Origin:  "manual",
				Key:     "fixture-name",
			},
		)
		require.NoError(t, profileErr)
	}
	invite := passbooking.Command{
		Name:             "invite",
		Event:            sandbox.RegistrationFixtureEventA,
		InviteTelegramID: 202,
		PaymentAdmin:     "bob",
		Key:              "fixture-invite",
	}
	booking, err := service.Execute(ctx, "alice", invite)
	require.NoError(t, err)
	require.NotNil(t, booking.InvitationStartedAt)
	first := *booking.InvitationStartedAt
	invite.Version, invite.Key = booking.Version, "fixture-repeat-invite"
	booking, err = service.Execute(ctx, "alice", invite)
	require.NoError(t, err)
	require.NotNil(t, booking.InvitationStartedAt)
	assert.Equal(t, first, *booking.InvitationStartedAt)
	f.Action = "read"
	state, err = sandbox.ApplyRegistrationFixture(ctx, db, f)
	require.NoError(t, err)
	encoded, err := json.Marshal(state)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "Private fixture name")
	for _, row := range state.Rows {
		if row.Event == sandbox.RegistrationFixtureEventA && row.Owner == "alice" {
			require.NotNil(t, row.Version)
			assert.Equal(t, booking.Version, *row.Version)
			require.NotNil(t, row.InvitationStartedAt)
			assert.Equal(t, first, *row.InvitationStartedAt)
			require.NotNil(t, row.IntentID)
			require.NotNil(t, row.Position)
		}
	}

	partner, err := service.Execute(ctx, "bob", passbooking.Command{
		Name: "accept", Event: sandbox.RegistrationFixtureEventA, Key: "fixture-accept",
		Target: "alice", TargetVersion: booking.Version,
	})
	require.NoError(t, err)
	booking, err = service.Get(ctx, "alice", sandbox.RegistrationFixtureEventA)
	require.NoError(t, err)
	assert.Equal(t, "bob", booking.Partner)
	assert.Equal(t, "alice", partner.Partner)
	assert.Equal(t, "assigned", booking.State)
	assert.Equal(t, "assigned", partner.State)
	require.NotNil(t, booking.Price)
	assert.Equal(t, 100, *booking.Price)
	queue, err := service.Queue(ctx, "visitor", sandbox.RegistrationFixtureEventA, "")
	require.NoError(t, err)
	assert.Len(t, queue.Bookings, 2)

	// The same domain service instance observes external revocation immediately.
	f.Action = "revoke-payment-a"
	_, err = sandbox.ApplyRegistrationFixture(ctx, db, f)
	require.NoError(t, err)
	caps, err := service.Capabilities(ctx, "bob", sandbox.RegistrationFixtureEventA)
	require.NoError(t, err)
	assert.NotContains(t, caps.Actions, "proof_accept")
	_, err = service.PaymentQueue(ctx, "bob", sandbox.RegistrationFixtureEventA, "")
	requireCode(t, err, "forbidden")
	_, err = service.Execute(
		ctx,
		"bob",
		passbooking.Command{
			Name:   "proof_accept",
			Event:  sandbox.RegistrationFixtureEventA,
			Key:    "fixture-revoked-proof",
			Target: "alice",
		},
	)
	requireCode(t, err, "forbidden")
	f.Action = "revoke-booking-admin"
	_, err = sandbox.ApplyRegistrationFixture(ctx, db, f)
	require.NoError(t, err)
	caps, err = service.Capabilities(ctx, "visitor", sandbox.RegistrationFixtureEventA)
	require.NoError(t, err)
	assert.NotContains(t, caps.Actions, "admin_assign")
	_, err = service.Queue(ctx, "visitor", sandbox.RegistrationFixtureEventA, "")
	requireCode(t, err, "forbidden")
	f.Action, f.OpensAt = "init", time.Now().Add(24*time.Hour)
	_, err = sandbox.ApplyRegistrationFixture(ctx, db, f)
	require.NoError(t, err)
	require.NoError(t, sandbox.ApplyProductFixture(ctx, db))
	caps, err = service.Capabilities(ctx, "bob", sandbox.RegistrationFixtureEventA)
	require.NoError(t, err)
	assert.NotContains(t, caps.Actions, "proof_accept")
	caps, err = service.Capabilities(ctx, "visitor", sandbox.RegistrationFixtureEventA)
	require.NoError(t, err)
	assert.NotContains(t, caps.Actions, "admin_assign")
	current, err := service.Get(ctx, "alice", sandbox.RegistrationFixtureEventA)
	require.NoError(t, err)
	assert.Equal(t, booking, current)
	var unchanged bool
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT starts_at<clock_timestamp() FROM core.pass_event_tiers WHERE event_id=$1 AND position=0`, sandbox.RegistrationFixtureEventA).
			Scan(&unchanged),
	)
	assert.True(t, unchanged)
	cancelled, cancelContext := context.WithCancel(ctx)
	cancelContext()
	f.Action, f.OpensAt = "read", time.Time{}
	_, err = sandbox.ApplyRegistrationFixture(cancelled, db, f)
	require.ErrorIs(t, err, context.Canceled)
}

func registrationFixtureDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("REGISTRATION_FIXTURE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip(
			"REGISTRATION_FIXTURE_TEST_DATABASE_URL requires the exclusively allocated registration fixture database",
		)
	}
	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	require.Equal(t, sandbox.RegistrationFixtureDatabase, cfg.ConnConfig.Database)
	require.Equal(t, "127.0.0.1", cfg.ConnConfig.Host)
	db, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	var allowed bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT current_database()=$1 AND current_user=pg_get_userbyid(datdba) FROM pg_database WHERE datname=current_database()`, sandbox.RegistrationFixtureDatabase).
			Scan(&allowed),
	)
	require.True(t, allowed)
	return db
}

func assertRegistrationFixtureRights(t *testing.T, service passbooking.Service) {
	t.Helper()
	for _, event := range []string{sandbox.RegistrationFixtureEventA, sandbox.RegistrationFixtureEventB} {
		alice, err := service.Capabilities(t.Context(), "alice", event)
		require.NoError(t, err)
		assert.Contains(t, alice.Actions, "solo")
		assert.NotContains(t, alice.Actions, "proof_accept")
		assert.NotContains(t, alice.Actions, "admin_assign")
		bob, err := service.Capabilities(t.Context(), "bob", event)
		require.NoError(t, err)
		assert.NotContains(t, bob.Actions, "admin_assign")
		assert.Equal(t, event == sandbox.RegistrationFixtureEventA, slices.Contains(bob.Actions, "proof_accept"))
		visitor, err := service.Capabilities(t.Context(), "visitor", event)
		require.NoError(t, err)
		assert.Contains(t, visitor.Actions, "admin_assign")
		assert.NotContains(t, visitor.Actions, "proof_accept")
	}
}
