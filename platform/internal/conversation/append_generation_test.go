package conversation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestHistoryGenerationIsReadOnlyAndObservesCommittedDeletion(t *testing.T) {
	t.Parallel()
	s := historyDatabase(t)
	cfg := s.DB.Config().Copy()
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	db, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	reader := conversation.Service{DB: db}
	generation, err := reader.Generation(t.Context(), "alice")
	require.NoError(t, err)
	require.Zero(t, generation)
	var summaries int
	require.NoError(t, s.DB.QueryRow(t.Context(),
		`SELECT count(*) FROM core.conversation_summaries WHERE owner='alice'`).Scan(&summaries))
	require.Zero(t, summaries)
	_, err = reader.Generation(t.Context(), "missing-owner")
	require.ErrorContains(t, err, "forbidden")
	id := longEvent(t, s, "committed history")
	generation, err = reader.Generation(t.Context(), "alice")
	require.NoError(t, err)
	require.Zero(t, generation)
	deleting, barrier := barrierService(t, s, "SELECT version FROM core.conversation_summaries")
	done := make(chan error, 1)
	go func() { done <- deleting.DeleteContent(t.Context(), "alice", id) }()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("deletion did not acquire its owner lock")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	generation, err = reader.Generation(ctx, "alice")
	cancel()
	close(barrier.release)
	require.NoError(t, <-done)
	require.NoError(t, err)
	require.Zero(t, generation)
	generation, err = reader.Generation(t.Context(), "alice")
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)
	require.ErrorContains(t,
		s.AppendDerived(t.Context(), "alice", "old-epoch", "stale reply", 0, []readsource.Authority{}), "history_stale")
	require.NoError(t,
		s.AppendDerived(t.Context(), "alice", "current-epoch", "current reply", generation, []readsource.Authority{}))
}

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
	err := s.AppendDerived(ctx, "alice", "held-append", "old generation reply", 0, []readsource.Authority{})
	cancel()
	close(barrier.release)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, <-done)
	require.ErrorContains(
		t,
		s.AppendDerived(t.Context(), "alice", "stale-append", "old generation reply", 0, []readsource.Authority{}),
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
	require.NoError(t, s.AppendDerived(t.Context(), "alice", "fresh-append", body, 1, []readsource.Authority{}))
	var saved string
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT body FROM core.conversation_message_bodies b JOIN core.conversation_events e ON e.id=b.event_id WHERE e.source_key='fresh-append'`).
			Scan(&saved),
	)
	require.Equal(t, body, saved)
	require.NoError(t, s.AppendOriginal(t.Context(), "alice", "manual-append", "manual", "button: menu"))
	require.NoError(t, s.AppendTrustedOutcome(t.Context(), "alice", "system-append", "notification"))
}
