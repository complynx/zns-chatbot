package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func TestAdminMessageRetriesSourceLockSerialization(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	blocker, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	_, err = blocker.Exec(t.Context(), `UPDATE core.users SET id=id WHERE id='bob'`)
	require.NoError(t, err)
	type result struct {
		message adminmessage.Message
		err     error
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	completed := make(chan result, 1)
	command := `/send_message_to 101 --msg "serialization recovery"`
	go func() {
		message, previewErr := service.PreviewCommand(ctx, "bob", "source-lock-retry", command)
		completed <- result{message: message, err: previewErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
WHERE datname=current_database() AND wait_event_type='Lock'
AND query LIKE '%FROM core.users%FOR NO KEY UPDATE%')`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, blocker.Commit(ctx))
	select {
	case got := <-completed:
		require.NoError(t, got.err)
		replay, replayErr := service.PreviewCommand(ctx, "bob", "source-lock-retry", command)
		require.NoError(t, replayErr)
		require.Equal(t, got.message.ID, replay.ID)
		var messages int
		require.NoError(t, db.QueryRow(ctx,
			`SELECT count(*) FROM core.admin_messages WHERE actor='bob' AND key='source-lock-retry'`).Scan(&messages))
		require.Equal(t, 1, messages)
	case <-ctx.Done():
		t.Fatal("preview did not recover after the source lock conflict")
	}
}
