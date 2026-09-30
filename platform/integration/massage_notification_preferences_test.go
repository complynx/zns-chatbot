package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func seedMassageFixtureNotice(t *testing.T, f *fixture, booking, owner, kind string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, f.db.QueryRow(t.Context(), `INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id)
 VALUES($1,$2,$3,$4) RETURNING id`, booking, owner, kind, f.b.Delivery.BotID).Scan(&id))
	indexSyntheticNotificationRows(t, f.db, "massage")
	return id
}

// The real adapter owns prepare, pacing, wire delivery and completion for each head.
func deliverMassageFixtureNotice(t *testing.T, f *fixture, id int64, state string) {
	t.Helper()
	require.Eventually(t, func() bool {
		require.NoError(t, f.b.DeliverMassageNotification(t.Context(), id))
		var settled bool
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT delivery_state=$2 AND NOT followup_pending
 FROM core.massage_notices WHERE id=$1`, id, state).Scan(&settled))
		return settled
	}, 5*time.Second, 5*time.Millisecond)
}

func TestMassageQueuedNoticeUsesCurrentSpecialistPreference(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"booked", "cancelled", "next"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := massageBotFixture(t)
			service := massage.Service{DB: f.db, Delivery: f.b.Delivery}
			_, err := service.SetPreferences(t.Context(), "bob", "sandbox-festival", massage.Preferences{})
			require.NoError(t, err)
			old, err := service.Execute(t.Context(), "alice", massageBook("preference-old", "bob", 4, 1))
			require.NoError(t, err)
			fresh, err := service.Execute(t.Context(), "alice", massageBook("preference-new", "bob", 6, 1))
			require.NoError(t, err)
			if kind == "cancelled" {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE core.massage_bookings SET cancelled_at=now() WHERE id=ANY($1)`,
					[]string{old.ID, fresh.ID},
				)
				require.NoError(t, err)
			}
			_, err = service.SetPreferences(
				t.Context(),
				"bob",
				old.Event,
				massage.Preferences{Bookings: true, Next: true},
			)
			require.NoError(t, err)
			oldID := seedMassageFixtureNotice(t, f, old.ID, "bob", kind)
			pending, err := service.PendingNotices(t.Context(), "bob")
			require.NoError(t, err)
			require.Len(t, pending, 1)
			preferences := massage.Preferences{Bookings: kind == "next", Next: kind != "next"}
			_, err = service.SetPreferences(t.Context(), "bob", old.Event, preferences)
			require.NoError(t, err)
			pending, err = service.PendingNotices(t.Context(), "bob")
			require.NoError(t, err)
			assert.Empty(t, pending)
			deliverMassageFixtureNotice(t, f, oldID, "cancelled")
			assert.Empty(t, chatMessages(t, f, 202))
			var sent bool
			require.NoError(t, f.db.QueryRow(t.Context(),
				`SELECT telegram_message_id<>0 OR delivery_text<>'' FROM core.massage_notices WHERE id=$1`,
				oldID).Scan(&sent))
			require.False(t, sent, "opt-out cancellation is not a successful send")
			_, err = service.SetPreferences(
				t.Context(),
				"bob",
				old.Event,
				massage.Preferences{Bookings: true, Next: true},
			)
			require.NoError(t, err)
			deliverMassageFixtureNotice(t, f, oldID, "cancelled")
			assert.Empty(t, chatMessages(t, f, 202), "re-enabling must not resurrect suppressed work")
			freshID := seedMassageFixtureNotice(t, f, fresh.ID, "bob", kind)
			deliverMassageFixtureNotice(t, f, freshID, "sent")
			assert.Len(t, chatMessages(t, f, 202), 1, "new eligible work must send after re-enabling")
			deliverMassageFixtureNotice(t, f, freshID, "sent")
			deliverMassageFixtureNotice(t, f, oldID, "cancelled")
			assert.Len(t, chatMessages(t, f, 202), 1, "neither completed nor cancelled work may resend")
		})
	}
}

func TestMassageSpecialistPreferencePreservesClientNotices(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	service := massage.Service{DB: f.db, Delivery: f.b.Delivery}
	_, err := service.SetPreferences(t.Context(), "bob", "sandbox-festival", massage.Preferences{})
	require.NoError(t, err)
	booking, err := service.Execute(t.Context(), "alice", massageBook("client-preferences", "bob", 4, 1))
	require.NoError(t, err)
	ids := []int64{}
	for _, kind := range []string{"prior_long", "prior_short", "additional"} {
		ids = append(ids, seedMassageFixtureNotice(t, f, booking.ID, "alice", kind))
	}
	pending, err := service.PendingNotices(t.Context(), "alice")
	require.NoError(t, err)
	assert.Len(t, pending, 3)
	for _, id := range ids {
		deliverMassageFixtureNotice(t, f, id, "sent")
	}
	assert.Len(t, chatMessages(t, f, 101), 3)
	assert.Empty(t, chatMessages(t, f, 202))
	for _, id := range ids {
		deliverMassageFixtureNotice(t, f, id, "sent")
	}
	assert.Len(t, chatMessages(t, f, 101), 3)
	assert.Empty(t, chatMessages(t, f, 202))
}
