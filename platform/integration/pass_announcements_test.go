package integration_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/destination"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRegistrationAnnouncementReplayAndRetry(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	s.AnnouncementBindings = &destination.Bindings{}
	require.NoError(
		t,
		s.AnnouncementBindings.Refresh(
			t.Context(),
			publicationResolver(func(context.Context, string) (int64, error) { return -100123, nil }),
			[]string{"@synthetic"},
			time.Minute,
		),
	)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET thread_channel='@synthetic',thread_id=42,thread_locale='ru'`,
	)
	require.NoError(t, err)
	command := bookingCommand("solo", "announce", passbooking.Booking{})
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := s.BeginRegistrationAnnouncement(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempts})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	assert.Equal(t, "-100123", item.Channel)
	assert.EqualValues(t, 42, *item.ThreadID)
	assert.Equal(t, "ru", item.Locale)
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(
		t,
		s.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{
				ID:      item.ID,
				Attempt: item.Attempts,
				Outcome: delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 120},
			},
		),
	)
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.False(t, found)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_registration_announcements SET name='Confirmed retry render input'`)
	require.NoError(t, err)
	retried, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, item.ID, retried.ID)
	assert.EqualValues(t, 2, retried.Attempts)
	assert.NotEqual(t, item.Text, retried.Text, "confirmed retry keeps its existing render policy")
	require.Error(
		t,
		s.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{
				ID:      item.ID,
				Attempt: item.Attempts,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 99},
			},
		),
	)
	gate, err = s.BeginRegistrationAnnouncement(
		t.Context(),
		delivery.Attempt{ID: retried.ID, Generation: retried.Attempts},
	)
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(
		t,
		s.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{
				ID:      item.ID,
				Attempt: retried.Attempts,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 44},
			},
		),
	)
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_registration_announcements`).Scan(&count),
	)
	assert.Equal(t, 1, count)
}

func TestRegistrationAnnouncementInterruptedSendSchedulesResend(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	s.AnnouncementBindings = &destination.Bindings{}
	require.NoError(
		t,
		s.AnnouncementBindings.Refresh(
			t.Context(),
			publicationResolver(func(context.Context, string) (int64, error) { return -100123, nil }),
			[]string{"@synthetic"},
			time.Minute,
		),
	)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "uncertain", passbooking.Booking{}))
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := s.BeginRegistrationAnnouncement(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempts})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET lease_until=clock_timestamp()-interval '2 minutes' WHERE id=$1`,
		item.ID,
	)
	require.NoError(t, err)
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
	var state string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM core.pass_registration_announcements WHERE id=$1`, item.ID).
			Scan(&state),
	)
	assert.Equal(t, "pending", state)
}

func TestRegistrationAnnouncementConcurrentClaims(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	s.AnnouncementBindings = &destination.Bindings{}
	require.NoError(
		t,
		s.AnnouncementBindings.Refresh(
			t.Context(),
			publicationResolver(func(context.Context, string) (int64, error) { return -100123, nil }),
			[]string{"@synthetic"},
			time.Minute,
		),
	)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='@synthetic'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "concurrent", passbooking.Booking{}))
	require.NoError(t, err)
	type result struct {
		found bool
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			_, found, claimErr := s.ClaimRegistrationAnnouncement(t.Context())
			results <- result{found: found, err: claimErr}
		}()
	}
	close(start)
	claimed := 0
	for range 2 {
		r := <-results
		require.NoError(t, r.err)
		if r.found {
			claimed++
		}
	}
	assert.Equal(t, 1, claimed)
}

func TestRegistrationAnnouncementRechecksDestinationAfterDelay(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	s.AnnouncementBindings = &destination.Bindings{}
	require.NoError(
		t,
		s.AnnouncementBindings.Refresh(
			t.Context(),
			publicationResolver(func(context.Context, string) (int64, error) { return -100123, nil }),
			[]string{"@synthetic"},
			time.Minute,
		),
	)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='@synthetic'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "changed-destination", passbooking.Booking{}))
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := s.BeginRegistrationAnnouncement(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempts})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(
		t,
		s.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{
				ID:      item.ID,
				Attempt: item.Attempts,
				Outcome: delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 60},
			},
		),
	)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET thread_channel='@replacement'; UPDATE core.pass_registration_announcements SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.False(t, found, "recovery cancels superseded pending work before another claim")
	var state string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM core.pass_registration_announcements WHERE id=$1`, item.ID).
			Scan(&state),
	)
	assert.Equal(t, "cancelled", state)
	var projected string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM core.delivery_queue WHERE owner_kind='announcement' AND owner_key=$1`, strconv.FormatInt(item.ID, 10)).
			Scan(&projected),
	)
	assert.Equal(t, "cancelled", projected)
}
func TestPassEventWithoutTiersRefusesAutomaticPrice(t *testing.T) {
	t.Parallel()
	db, s := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`DELETE FROM core.pass_event_tiers WHERE event_id='dance';UPDATE core.pass_events SET default_price=12500,amount_cap_per_role=1 WHERE id='dance'`,
	)
	require.NoError(t, err)
	_, err = s.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "event-default",
			Version:       1,
			Target:        "alice",
			TargetVersion: 1,
		},
	)
	require.ErrorContains(t, err, "pass_tier_unavailable")
}
