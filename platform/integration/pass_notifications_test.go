package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func drainPassDomainNotices(t *testing.T, service passbooking.Service) []passbooking.Notification {
	t.Helper()
	result := []passbooking.Notification{}
	for range 100 {
		notices, err := service.PendingNotifications(t.Context())
		require.NoError(t, err)
		if len(notices) == 0 {
			return result
		}
		for _, notice := range notices {
			result = append(result, notice)
			require.NoError(t, service.CompleteNotification(t.Context(), notice.ID, ""))
		}
	}
	t.Fatal("pass notification drain exceeded bound")
	return nil
}

func noticeKinds(notices []passbooking.Notification, recipient string) []string {
	kinds := []string{}
	for _, notice := range notices {
		if notice.Recipient == recipient && notice.Current {
			kinds = append(kinds, notice.Kind)
		}
	}
	return kinds
}

func TestPassNotificationsRegistrationPairAndAssignment(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET titles='{"en":"Dance event","ru":"Танцы"}'`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err, "replay cannot enqueue another invitation")
	notices := drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"registered"}, noticeKinds(notices, "alice"))
	assert.Equal(t, []string{"invitation"}, noticeKinds(notices, "bob"))
	for _, notice := range notices {
		assert.Equal(t, "alice", notice.Owner)
		assert.Equal(t, "Dance event", notice.EventTitles["en"])
		assert.Equal(t, "Танцы", notice.EventTitles["ru"])
		if notice.Recipient == "bob" {
			assert.EqualValues(t, 202, notice.TelegramID)
		}
	}
	accept := bookingCommand("accept", "accept", passbooking.Booking{})
	accept.Target, accept.TargetVersion = "alice", alice.Version
	_, err = service.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	notices = drainPassDomainNotices(t, service)
	assert.ElementsMatch(t, []string{"pair_accepted", "assigned"}, noticeKinds(notices, "alice"))
	assert.ElementsMatch(t, []string{"registered", "pair_accepted", "assigned"}, noticeKinds(notices, "bob"))
	require.Empty(t, drainPassDomainNotices(t, service))
}

func TestPassNotificationsPaymentAndSupersededGeneration(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("synthetic receipt"))
	require.NoError(t, err)
	submit := bookingCommand("proof", "submit", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	notices := drainPassDomainNotices(t, service)
	assert.Contains(t, noticeKinds(notices, "bob"), "payment_request")
	for _, notice := range notices {
		if notice.Kind == "assigned" {
			assert.False(t, notice.Current, "receipt supersedes unpaid assignment notice")
		}
	}
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	review := bookingCommand("proof_reject", "reject", passbooking.Booking{})
	review.Target, review.TargetVersion, review.PaymentAttempt = "alice", alice.Version, payment.Attempt
	_, err = service.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	notices = drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"payment_rejected"}, noticeKinds(notices, "alice"))
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	submit = bookingCommand("proof", "submit2", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	price := 0
	_, err = service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "free",
			Target:        "alice",
			TargetVersion: alice.Version,
			TotalPrice:    &price,
		},
	)
	require.NoError(t, err)
	notices = drainPassDomainNotices(t, service)
	assert.Contains(t, noticeKinds(notices, "alice"), "free_assigned")
	for _, notice := range notices {
		if notice.Kind == "payment_request" {
			assert.False(t, notice.Current, "replaced receipt cannot solicit review")
		}
	}
}

func TestPassNotificationsDurableRetryAndCurrentData(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	notices, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1, "oldest per recipient only")
	first := notices[0]
	require.NoError(t, service.CompleteNotification(t.Context(), first.ID, "telegram_retry"))
	notices, err = service.PendingNotifications(t.Context())
	require.NoError(t, err)
	assert.Empty(t, notices, "backoff on first notice also holds later recipient notices")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_notifications SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.pass_events SET titles='{"en":"Renamed event","ru":"Новое событие"}'`,
	)
	require.NoError(t, err)
	restarted := passbooking.Service{DB: db}
	notices, err = restarted.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, first.ID, notices[0].ID)
	assert.Equal(t, "Renamed event", notices[0].EventTitles["en"])
	require.NoError(t, restarted.CompleteNotification(t.Context(), first.ID, "telegram_forbidden"))
	require.NoError(t, restarted.CompleteNotification(t.Context(), first.ID, ""), "ack replay")
	notices, err = restarted.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, "assigned", notices[0].Kind)
	_, err = service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{Event: "dance", Key: "stale", Target: "alice", TargetVersion: 1},
	)
	require.NoError(t, err)
	require.NoError(t, restarted.CompleteNotification(t.Context(), notices[0].ID, ""))
	requireCode(t, restarted.CompleteNotification(t.Context(), -1, ""), "pass_booking_invalid")
	requireCode(t, restarted.CompleteNotification(t.Context(), 99999, ""), "notification_not_found")
}

func TestPassNotificationsDeclineAndWaitlistOnce(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	decline := bookingCommand("decline", "decline", passbooking.Booking{})
	decline.Target, decline.TargetVersion = "alice", alice.Version
	_, err = service.Execute(t.Context(), "bob", decline)
	require.NoError(t, err)
	notices := drainPassDomainNotices(t, service)
	assert.ElementsMatch(t, []string{"pair_declined", "waitlisted"}, noticeKinds(notices, "alice"))
	_, err = service.Execute(t.Context(), "bob", bookingCommand("recalculate", "again", passbooking.Booking{}))
	require.NoError(t, err)
	require.Empty(t, drainPassDomainNotices(t, service), "unchanged blocked queue does not repeatedly notify")
}

func TestPassNotificationsExistingWaitlistOnceWithoutVersionChange(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	command := bookingCommand("recalculate", "first", passbooking.Booking{Version: 1})
	bob, err := service.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.EqualValues(t, 1, bob.Version)
	notices := drainPassDomainNotices(t, service)
	assert.Equal(t, []string{"waitlisted"}, noticeKinds(notices, "alice"))
	assert.Equal(t, []string{"waitlisted"}, noticeKinds(notices, "bob"))
	command.Key = "second"
	_, err = service.Execute(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Empty(t, drainPassDomainNotices(t, service))
}
