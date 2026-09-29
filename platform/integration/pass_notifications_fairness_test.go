package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassNotificationsRetryDoesNotStarveOtherRecipients(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	// Use a normal registration payload for the synthetic recipient batch.
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
 SELECT 'notice-'||i,90000+i,'Recipient '||i,true FROM generate_series(1,26) i;
 INSERT INTO core.pass_notifications(event_id,owner,recipient,kind,generation,payload,available_at,bot_id)
 SELECT n.event_id,n.owner,'notice-'||i,n.kind,n.generation,n.payload,clock_timestamp()-interval '1 day',n.bot_id
 FROM core.pass_notifications n CROSS JOIN generate_series(1,26) i
 WHERE n.kind='registered' ORDER BY i;
 DELETE FROM core.pass_notifications WHERE recipient='alice'`)
	require.NoError(t, err)
	indexSyntheticNotificationRows(t, db, "registration")
	first, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, first, 25)
	for _, notice := range first {
		assert.NotEqual(t, "notice-26", notice.Recipient)
		require.NoError(t, deferPassTestNotice(t.Context(), service, notice))
	}
	// Emulate a slow poll: retries are eligible again, but untouched work is older.
	_, err = db.Exec(t.Context(), `UPDATE core.pass_notifications SET available_at=clock_timestamp()-interval '1 minute'
 WHERE recipient<>'notice-26'`)
	require.NoError(t, err)
	next, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, next, 25)
	assert.Equal(t, "notice-26", next[0].Recipient)
	assert.True(t, next[0].Current)
}
