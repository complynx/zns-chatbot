package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRegistrationAnnouncementReplayAndRetry(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
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
	assert.Equal(t, "@synthetic", item.Channel)
	assert.EqualValues(t, 42, *item.ThreadID)
	assert.Equal(t, "ru", item.Locale)
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(
		t,
		s.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{ID: item.ID, Failure: "telegram_rate_limit"},
		),
	)
	retried, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, item.ID, retried.ID)
	assert.Equal(t, 2, retried.Attempts)
	require.NoError(
		t,
		s.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{ID: item.ID, MessageID: 44}),
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

func TestRegistrationAnnouncementInterruptedClaimBecomesUnknown(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "uncertain", passbooking.Booking{}))
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET available_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`,
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
	assert.Equal(t, "unknown", state)
}

func TestRegistrationAnnouncementConcurrentClaims(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
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
