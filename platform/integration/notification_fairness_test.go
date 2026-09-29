package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestNotificationRetriesDoNotStarveNewRecipients(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, _ := cashOrder(t, f, "fair-order-queue")
	_, err := f.db.Exec(t.Context(), `UPDATE core.order_notifications SET delivered_at=now()`)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name)
	SELECT 'retry-'||n,1000+n,'Synthetic recipient' FROM generate_series(1,26) n`)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `INSERT INTO core.order_notifications(recipient,order_id,payload,available_at)
	SELECT 'retry-'||n,$1,'{"kind":"created"}'::jsonb,clock_timestamp()-interval '1 minute'
	FROM generate_series(1,26) n ORDER BY n`, order.ID)
	require.NoError(t, err)
	service := orders.Service{DB: f.db}
	first, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, first, 25)
	for _, notice := range first {
		require.NoError(t, service.CompleteNotification(t.Context(), notice.ID, "telegram_retry"))
	}
	// Simulate the next poll arriving after the retry cooldown, without sleeping.
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_notifications
	SET available_at=available_at-interval '6 seconds' WHERE recipient<>'retry-26' AND delivered_at IS NULL`)
	require.NoError(t, err)
	next, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, next)
	assert.Equal(t, "retry-26", next[0].Recipient)
}
