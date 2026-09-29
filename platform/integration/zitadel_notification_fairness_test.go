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
	service := massage.Service{DB: f.db}
	booking, err := service.Execute(t.Context(), "alice", massageBook("fair-notices", "bob", 4, 1))
	require.NoError(t, err)
	enableRuntimeIdentity(t, f)
	// More failed recipients than both the delivery batch and recipient query limit.
	const failedRecipients = 101
	for index := range failedRecipients {
		owner := fmt.Sprintf("a-failed-%03d", index)
		_, err = f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name)
		VALUES($1,$2,'Synthetic unavailable recipient')`, owner, 1000+index)
		require.NoError(t, err)
		_, err = f.db.Exec(t.Context(), `INSERT INTO core.massage_notices(booking_id,owner,kind)
		VALUES($1,$2,'booked')`, booking.ID, owner)
		require.NoError(t, err)
	}
	for range 12 {
		// Reconstruct the bot between polls: fairness must not depend on memory.
		restarted := *f.b
		require.NoError(t, restarted.DeliverMassageNotifications(t.Context()))
	}
	var sent bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT sent_at IS NOT NULL
	FROM core.massage_notices WHERE booking_id=$1 AND owner='bob' AND kind='booked'`, booking.ID).Scan(&sent))
	assert.True(t, sent)
	var failedPending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices
	WHERE owner LIKE 'a-failed-%' AND sent_at IS NULL`).Scan(&failedPending))
	assert.Equal(t, failedRecipients, failedPending)
}
