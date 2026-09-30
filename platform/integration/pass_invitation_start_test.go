package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassInvitationStartsAfterLongWaitlist(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	booking, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	require.Equal(t, "waitlist", booking.State)
	// Keep maintenance selecting this event while Alice's invitation is fresh.
	// This exercises the Go deadline check independently of the SQL pre-filter.
	waiting, err := service.Execute(t.Context(), "bob", bookingCommand("solo", "co-booking", passbooking.Booking{}))
	require.NoError(t, err)
	require.Equal(t, "waitlist", waiting.State)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET created_at=clock_timestamp()-interval '90 hours' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	invite := bookingCommand("invite", "invite-after-queue", booking)
	invite.InviteTelegramID = 202
	booking, err = service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	require.Equal(t, "waiting-for-couple", booking.State)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	booking, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(
		t,
		"waiting-for-couple",
		booking.State,
		"waiting in the registration queue must not consume the invitation window",
	)
	require.NotNil(t, booking.InvitationStartedAt)
	assert.Greater(t, booking.InvitationStartedAt.Sub(booking.CreatedAt), 89*time.Hour)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET invitation_started_at=clock_timestamp()-interval '57 hours' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	booking, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, "waiting-for-couple", booking.State)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET invitation_started_at=clock_timestamp()-interval '59 hours' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	booking, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "waitlist", booking.State)
	assert.Nil(t, booking.InvitationStartedAt)
}

func TestPassInvitationReplayDoesNotExtendWindow(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	invite := bookingCommand("invite", "initial", passbooking.Booking{})
	invite.InviteTelegramID = 202
	_, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET invitation_started_at=clock_timestamp()-interval '24 hours' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	initial, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.NotNil(t, initial.InvitationStartedAt)
	// Read the stored epoch through another service instance; this is not a process restart.
	service = passbooking.Service{DB: db, Delivery: service.Delivery}
	replay, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	assert.Equal(t, initial.InvitationStartedAt, replay.InvitationStartedAt)
	same := bookingCommand("invite", "same-target-new-command", replay)
	same.InviteTelegramID = 202
	repeated, err := service.Execute(t.Context(), "alice", same)
	require.NoError(t, err)
	assert.Equal(t, initial.InvitationStartedAt, repeated.InvitationStartedAt)
	assert.Equal(t, initial.CreatedAt, repeated.CreatedAt)
	// A different target starts a genuinely new invitation, not a new registration.
	different := bookingCommand("invite", "different-target", repeated)
	different.InviteTelegramID = 303
	var earliest, latest time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&earliest))
	changed, err := service.Execute(t.Context(), "alice", different)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&latest))
	require.NotNil(t, changed.InvitationStartedAt)
	assert.False(t, changed.InvitationStartedAt.Before(earliest))
	assert.False(t, changed.InvitationStartedAt.After(latest))
	assert.Equal(t, initial.CreatedAt, changed.CreatedAt)
}

func TestPassInvitationExitClearsStart(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"solo", "cancel", "accept", "decline", "admin_cancel"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			db, service := bookingFixture(t)
			_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
			require.NoError(t, err)
			invite := bookingCommand("invite", "initial", passbooking.Booking{})
			invite.InviteTelegramID = 202
			inviter, err := service.Execute(t.Context(), "alice", invite)
			require.NoError(t, err)
			require.NotNil(t, inviter.InvitationStartedAt)
			actor := "alice"
			command := bookingCommand(action, "leave-invitation", inviter)
			if action == "accept" || action == "decline" || action == "admin_cancel" {
				actor = "bob"
				command = bookingCommand(action, "reply-to-invitation", passbooking.Booking{})
				command.Target = "alice"
				command.TargetVersion = inviter.Version
			}
			result, err := service.Execute(t.Context(), actor, command)
			require.NoError(t, err)
			if action != "decline" && action != "admin_cancel" {
				assert.Nil(t, result.InvitationStartedAt)
			}
			inviter, err = service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			require.Nil(t, inviter.InvitationStartedAt)
			if action == "accept" {
				return
			}
			renewed := bookingCommand("invite", "new-invitation-after-exit", inviter)
			renewed.InviteTelegramID = 202
			result, err = service.Execute(t.Context(), "alice", renewed)
			require.NoError(t, err)
			assert.NotNil(t, result.InvitationStartedAt)
		})
	}
}

func TestPassInvitationLegacyUnknownStartKeepsDeadline(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "initial", passbooking.Booking{})
	invite.InviteTelegramID = 202
	booking, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	// Import and migration cannot invent the actual start of historical invitations.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET invitation_started_at=NULL,created_at=clock_timestamp()-interval '59 hours' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	same := bookingCommand("invite", "same-legacy-invitation", booking)
	same.InviteTelegramID = 202
	booking, err = service.Execute(t.Context(), "alice", same)
	require.NoError(t, err)
	require.Nil(
		t,
		booking.InvitationStartedAt,
		"repeating an unknown historical invitation must not reset its deadline",
	)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	booking, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "waitlist", booking.State)
	assert.Nil(t, booking.InvitationStartedAt)
}
