package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestMassageQueuedNoticeUsesCurrentSpecialistPreference(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"booked", "cancelled", "next"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := massageBotFixture(t)
			service := massage.Service{DB: f.db}
			booking, err := service.Execute(t.Context(), "alice", massageBook("preference-recheck", "bob", 4, 1))
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `DELETE FROM core.massage_notices WHERE booking_id=$1`, booking.ID)
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.massage_notices(booking_id,owner,kind) VALUES($1,'bob',$2)`,
				booking.ID,
				kind,
			)
			require.NoError(t, err)
			if kind == "cancelled" {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE core.massage_bookings SET cancelled_at=now() WHERE id=$1`,
					booking.ID,
				)
				require.NoError(t, err)
			}
			preferences := massage.Preferences{Bookings: kind == "next", Next: kind != "next"}
			_, err = service.SetPreferences(t.Context(), "bob", booking.Event, preferences)
			require.NoError(t, err)
			pending, err := service.PendingNotices(t.Context(), "bob")
			require.NoError(t, err)
			assert.Empty(t, pending)
			delivery, err := service.DeliveryNotices(t.Context(), "bob")
			require.NoError(t, err)
			assert.Empty(t, delivery)
			recipients, err := service.NoticeRecipients(t.Context())
			require.NoError(t, err)
			for _, recipient := range recipients {
				assert.NotEqual(t, "bob", recipient.Owner)
			}
			require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
			assert.Empty(t, chatMessages(t, f, 202))
			// Preference filtering must not acknowledge an unsent notice.
			_, err = service.SetPreferences(
				t.Context(),
				"bob",
				booking.Event,
				massage.Preferences{Bookings: true, Next: true},
			)
			require.NoError(t, err)
			require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
			assert.Len(t, chatMessages(t, f, 202), 1)
			require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
			assert.Len(t, chatMessages(t, f, 202), 1)
		})
	}
}

func TestMassageSpecialistPreferencePreservesClientNotices(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	service := massage.Service{DB: f.db}
	booking, err := service.Execute(t.Context(), "alice", massageBook("client-preferences", "bob", 4, 1))
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.massage_notices(booking_id,owner,kind) VALUES($1,'alice','prior_long'),($1,'alice','prior_short'),($1,'alice','additional')`,
		booking.ID,
	)
	require.NoError(t, err)
	_, err = service.SetPreferences(t.Context(), "bob", booking.Event, massage.Preferences{})
	require.NoError(t, err)
	pending, err := service.PendingNotices(t.Context(), "alice")
	require.NoError(t, err)
	assert.Len(t, pending, 3)
	delivery, err := service.DeliveryNotices(t.Context(), "alice")
	require.NoError(t, err)
	assert.Len(t, delivery, 3)
	require.NoError(t, f.b.DeliverMassageNotifications(t.Context()))
	assert.Len(t, chatMessages(t, f, 101), 3)
	assert.Empty(t, chatMessages(t, f, 202))
}
