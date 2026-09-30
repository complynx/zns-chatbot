package integration_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassNotificationsDeadlineMarkerRelativeAndConcurrent(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET assigned_at=clock_timestamp()-interval '20 days' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() { _, processErr := service.ProcessDeadlines(t.Context()); results <- processErr })
	}
	group.Wait()
	close(results)
	for processErr := range results {
		require.NoError(t, processErr)
	}
	notices := drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"reminder_first"}, noticeKinds(notices, "alice"))
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", alice.State, "long downtime does not skip the first-marker grace period")
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainPassDomainNotices(t, service))
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_deadline_markers SET first_at=clock_timestamp()-interval '25 hours'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	notices = drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"reminder_second"}, noticeKinds(notices, "alice"))
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_deadline_markers SET first_at=clock_timestamp()-interval '49 hours'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	notices = drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"deadline_cancelled"}, noticeKinds(notices, "alice"))
}

func TestPassNotificationsInvitationExpiryAndPaidSafety(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	_, err = service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET invitation_started_at=clock_timestamp()-interval '59 hours' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "waitlist", alice.State)
	assert.Zero(t, alice.InvitationTarget)
	assert.Equal(t, "solo", alice.Kind)
	notices := drainPassDomainNotices(t, service)
	assert.Contains(t, noticeKinds(notices, "alice"), "invitation_expired")
	assert.Contains(t, noticeKinds(notices, "alice"), "waitlisted")
	assert.Equal(t, []string{"invitation_expired"}, noticeKinds(notices, "bob"))
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainPassDomainNotices(t, service))
	free := 0
	_, err = service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "free",
			Target:        "alice",
			TargetVersion: alice.Version,
			TotalPrice:    &free,
		},
	)
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET assigned_at=clock_timestamp()-interval '100 days' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "paid", alice.State)
	require.Empty(t, drainPassDomainNotices(t, service))
}

func TestPassNotificationsOldReminderStaleAfterReassignment(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET assigned_at=clock_timestamp()-interval '7 days' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	price := 75
	_, err = service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "replacement",
			Target:        "alice",
			TargetVersion: alice.Version,
			TotalPrice:    &price,
		},
	)
	require.NoError(t, err)
	notices := drainPassDomainNotices(t, service)
	for _, notice := range notices {
		if notice.Kind == "reminder_first" {
			assert.False(t, notice.Current)
		}
	}
	assert.Equal(t, []string{"assigned"}, noticeKinds(notices, "alice"))
	count, err := service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count, "replacement assignment starts a new deadline generation")
}

func TestPassNotificationsDeadlineAtomicFailureAndPaidPartner(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state=CASE WHEN owner='bob' THEN 'paid' ELSE 'assigned' END,
 assigned_at=clock_timestamp()-interval '10 days',price=CASE WHEN owner='bob' THEN 200 ELSE 100 END;
 CREATE FUNCTION core.fail_pass_reminder() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.kind='reminder_first' THEN RAISE EXCEPTION 'synthetic outbox failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fail_pass_reminder BEFORE INSERT ON core.pass_notifications FOR EACH ROW EXECUTE FUNCTION core.fail_pass_reminder()`,
	)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.Error(t, err)
	var markers int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_deadline_markers`).Scan(&markers))
	assert.Zero(t, markers, "deadline marker cannot commit without durable notification")
	_, err = db.Exec(t.Context(), `DROP TRIGGER fail_pass_reminder ON core.pass_notifications`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_deadline_markers SET first_at=clock_timestamp()-interval '3 days'`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	assert.Equal(t, "paid", bob.State)
	assert.Equal(t, 200, *bob.Price)
	assert.Empty(t, bob.Partner)
	notices := drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"deadline_cancelled"}, noticeKinds(notices, "alice"))
	assert.Equal(t, []string{"pair_changed"}, noticeKinds(notices, "bob"))
}
