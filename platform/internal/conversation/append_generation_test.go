package conversation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHistoryAppendGenerationSharesDeletionLock(t *testing.T) {
	t.Parallel()
	s := historyDatabase(t)
	id := longEvent(t, s, strings.Repeat("archival canary 🌍", 500))
	deleting, barrier := barrierService(t, s, "SELECT version FROM core.conversation_summaries")
	done := make(chan error, 1)
	go func() { done <- deleting.DeleteContent(t.Context(), "alice", id) }()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not acquire its owner lock")
	}
	// Deletion holds its first lock before incrementing generation. An append
	// cannot accept the still-visible old generation while this lock is held.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	err := s.AppendAtGeneration(ctx, "alice", "held-append", "assistant", "old generation reply", 0)
	cancel()
	close(barrier.release)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, <-done)
	require.ErrorContains(
		t,
		s.AppendAtGeneration(t.Context(), "alice", "stale-append", "assistant", "old generation reply", 0),
		"history_stale",
	)
	var count int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key IN ('held-append','stale-append')`).
			Scan(&count),
	)
	require.Zero(t, count)
	body := strings.Repeat("fresh complete reply 🌍", 500)
	require.NoError(t, s.AppendAtGeneration(t.Context(), "alice", "fresh-append", "assistant", body, 1))
	var saved string
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT body FROM core.conversation_message_bodies b JOIN core.conversation_events e ON e.id=b.event_id WHERE e.source_key='fresh-append'`).
			Scan(&saved),
	)
	require.Equal(t, body, saved)
	require.NoError(t, s.Append(t.Context(), "alice", "manual-append", "manual", "button: menu"))
	require.NoError(t, s.Append(t.Context(), "alice", "system-append", "system", "notification"))
}
