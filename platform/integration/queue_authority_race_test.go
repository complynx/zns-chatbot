package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestQueueAuthorityArchiveRechecksPendingDecisionAfterLock(t *testing.T) {
	t.Parallel()
	s, authority := queuePaymentAuthorityFixture(t)
	tx, err := s.DB.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = tx.Exec(
		t.Context(),
		`UPDATE core.pass_payment_attempts SET decision='accepted',reviewed_by='bob',reviewed_at=clock_timestamp() WHERE id=$1`,
		authority.PaymentAttempt,
	)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- s.AppendDerived(t.Context(), "bob", "queue-race", "stale pending review", 0, readsource.Registration([]passbooking.ReadAuthority{authority}))
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := s.DB.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockReadPaymentAttempt%')`).
			Scan(&waiting)
		return queryErr == nil && waiting
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(t.Context()))
	requireCode(t, <-done, "history_stale")
	var count int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key='queue-race'`).
			Scan(&count),
	)
	assert.Zero(t, count)
}
