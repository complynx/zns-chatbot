package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestAdminUtilitiesRefreshBindsUnnotifiedRegistration(t *testing.T) {
	t.Parallel()
	db, bookings := bookingFixture(t)
	_, err := bookings.Execute(t.Context(), "alice", bookingCommand("solo", "refresh-delivery", passbooking.Booking{}))
	require.NoError(t, err)
	// Model an imported eligible booking with no announcement receipt.
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_registration_announcements WHERE owner='alice';
UPDATE core.pass_events SET thread_channel='@synthetic_refresh',thread_id=42;
UPDATE core.pass_bookings SET assigned_at=clock_timestamp()-interval '7 days' WHERE owner='alice'`)
	require.NoError(t, err)
	settings := syntheticDeliverySettings()
	services := appservices.NewServices(db, appservices.Options{LegacyOrderBotID: settings.BotID, Delivery: settings})
	_, err = services.AdminUtilities.Refresh(t.Context(), "bob")
	require.NoError(t, err)
	var boundBot int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT COALESCE(bot_id,0) FROM core.pass_registration_announcements WHERE owner='alice' AND event_id='dance'`).
			Scan(&boundBot),
	)
	require.Equal(t, settings.BotID, boundBot)
	item, found, err := services.Registration.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "@synthetic_refresh", item.Channel)
	gate, err := services.Registration.BeginRegistrationAnnouncement(
		t.Context(),
		delivery.Attempt{ID: item.ID, Generation: item.Attempts},
	)
	require.NoError(t, err)
	assert.True(t, gate.Ready)
}
