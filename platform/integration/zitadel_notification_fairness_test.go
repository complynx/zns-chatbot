package integration_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func TestZitadelMassageNotificationsRotateFailedRecipients(t *testing.T) {
	t.Parallel()
	f := massageBotFixture(t)
	service := massage.Service{DB: f.db, Delivery: f.b.Delivery}
	booking, err := service.Execute(t.Context(), "alice", massageBook("fair-notices", "bob", 4, 1))
	require.NoError(t, err)
	enableRuntimeIdentity(t, f)
	// More failed recipients than both the delivery batch and recipient query limit.
	const failedRecipients = 101
	for index := range failedRecipients {
		owner := fmt.Sprintf("a-failed-%03d", index)
		_, err = f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
		VALUES($1,$2,'Synthetic unavailable recipient',true)`, owner, 1000+index)
		require.NoError(t, err)
		_, err = f.db.Exec(t.Context(), `INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id)
		VALUES($1,$2,'booked',$3)`, booking.ID, owner, f.b.Delivery.BotID)
		require.NoError(t, err)
	}
	indexSyntheticNotificationRows(t, f.db, "massage")
	var eligible int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices n
JOIN core.delivery_queue q ON q.bot_id=n.bot_id AND q.owner_kind='massage'
AND q.owner_key=n.id::text AND q.effect_key='send'
JOIN core.users u ON u.id=n.owner
WHERE n.owner LIKE 'a-failed-%' AND n.bot_id=$1 AND n.delivery_chat>0 AND u.can_book
AND q.chat=n.delivery_chat::text AND n.delivery_state='pending' AND q.state='pending'
AND n.available_at<=clock_timestamp() AND n.delivery_attempt=0`, f.b.Delivery.BotID).Scan(&eligible))
	require.Equal(t, failedRecipients, eligible)
	first, err := service.NoticeRecipients(t.Context())
	require.NoError(t, err)
	require.Len(t, first, 100)
	for _, recipient := range first {
		require.Contains(t, recipient.Owner, "a-failed-")
	}
	for range 12 {
		// Reconstruct the bot between polls: fairness must not depend on memory.
		restarted := *f.b
		require.NoError(t, restarted.DeliverMassageNotifications(t.Context()))
	}
	var state string
	var messageID int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT delivery_state,telegram_message_id
	FROM core.massage_notices WHERE booking_id=$1 AND owner='bob' AND kind='booked'`, booking.ID).Scan(&state, &messageID))
	require.Equal(t, "sent", state)
	require.Positive(t, messageID)
	visible := false
	for _, message := range chatMessages(t, f, 202) {
		if message.ID == messageID {
			visible = true
			require.NotEmpty(t, message.Text)
		}
	}
	require.True(t, visible, "the successful receipt must identify Bob's actual Telegram message")
	var failedPending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices
	WHERE owner LIKE 'a-failed-%' AND sent_at IS NULL AND delivery_attempt>0
	AND delivery_state='pending' AND failure='notification_preflight_unavailable'`).Scan(&failedPending))
	assert.Equal(t, failedRecipients, failedPending)
}
