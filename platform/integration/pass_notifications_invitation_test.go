package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassNotificationsInvitationGenerationOffline(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name) VALUES('carol',90909,'Carol');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','carol')`)
	require.NoError(t, err)
	var alice passbooking.Booking
	for index, target := range []int64{202, 90909, 202} {
		invite := bookingCommand("invite", []string{"first", "second", "third"}[index], alice)
		invite.InviteTelegramID = target
		invite.PaymentAdmin = "bob"
		alice, err = service.Execute(t.Context(), "alice", invite)
		require.NoError(t, err)
	}
	var newest int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT max(id) FROM core.pass_notifications WHERE kind='invitation'`).Scan(&newest),
	)
	metadata := bookingCommand("payment_admin", "metadata", alice)
	metadata.PaymentAdmin = "carol"
	updated, err := service.Execute(t.Context(), "alice", metadata)
	require.NoError(t, err)
	assert.Greater(t, updated.Version, alice.Version)
	assert.Equal(t, alice.CreatedAt, updated.CreatedAt)
	notices := drainPassDomainNotices(t, service)
	var invitationCount int
	var currentIDs []int64
	for _, notice := range notices {
		if notice.Kind != "invitation" {
			continue
		}
		invitationCount++
		if notice.Current {
			currentIDs = append(currentIDs, notice.ID)
			assert.Equal(t, "bob", notice.Recipient)
			assert.Equal(t, updated.Version, notice.Version)
		}
	}
	assert.Equal(t, 3, invitationCount)
	assert.Equal(t, []int64{newest}, currentIDs, "only the latest invitation can be delivered after an outage")
}
