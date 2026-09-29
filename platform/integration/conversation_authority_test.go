package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func conversationAuthorityFixture(t *testing.T) (conversation.Service, passbooking.ReadAuthority) {
	t.Helper()
	db, passes := bookingFixture(t)
	booking, err := passes.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "authority-booking", passbooking.Booking{}),
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_registration_announcements WHERE event_id='dance'`)
	require.NoError(t, err)
	return conversation.Service{
		DB: db,
	}, passbooking.ReadAuthority{
		Kind:      passbooking.ReadOwnerBooking,
		Event:     booking.Event,
		Owner:     booking.Owner,
		Version:   booking.Version,
		CreatedAt: booking.CreatedAt.UTC(),
	}
}

func TestConversationAuthorityRetainsBodiesUntilRevoked(t *testing.T) {
	t.Parallel()
	s, authority := conversationAuthorityFixture(t)
	text := strings.Repeat("authorized registration detail 🌍 ", 300)
	require.NoError(t, s.AppendOriginal(t.Context(), "alice", "original", "assistant", text))
	require.NoError(
		t,
		s.AppendDerived(
			t.Context(),
			"alice",
			"derived",
			text,
			0,
			readsource.Registration([]passbooking.ReadAuthority{authority}),
		),
	)
	_, err := s.DB.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	page, err := s.Read(t.Context(), "alice", conversation.Query{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	derivedID := page.Events[0].ID
	assert.Equal(t, readsource.Registration([]passbooking.ReadAuthority{authority}), page.Events[0].ReadAuthorities)
	body, err := s.ReadText(t.Context(), "alice", derivedID, 0, conversation.MaxChunkCharacters, "")
	require.NoError(t, err)
	assert.Contains(t, body.Text, "authorized registration detail")
	assert.True(t, body.More)
	assert.Equal(t, readsource.Registration([]passbooking.ReadAuthority{authority}), body.ReadAuthorities)
	_, err = s.DB.Exec(t.Context(), `DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'`)
	require.NoError(t, err)
	window, err := s.Window(t.Context(), "alice", 2)
	require.NoError(t, err)
	assert.EqualValues(t, 1, window.Generation)
	assert.True(t, window.Recent[1].Omitted)
	assert.Equal(t, "authority_revoked", window.Recent[1].OmissionReason)
	assert.False(t, window.Recent[0].Omitted)
	body, err = s.ReadText(t.Context(), "alice", derivedID, 0, 100, "")
	require.NoError(t, err)
	assert.Empty(t, body.Text)
	original, err := s.ReadText(t.Context(), "alice", page.Events[1].ID, 0, 100, "")
	require.NoError(t, err)
	assert.Contains(t, original.Text, "authorized registration detail")
	requireCode(
		t,
		s.AppendDerived(
			t.Context(),
			"alice",
			"late",
			"must not commit",
			0,
			readsource.Registration([]passbooking.ReadAuthority{authority}),
		),
		"history_stale",
	)
}

func TestConversationAuthoritySummaryAndInheritedReply(t *testing.T) {
	t.Parallel()
	s, authority := conversationAuthorityFixture(t)
	require.NoError(
		t,
		s.AppendDerived(
			t.Context(),
			"alice",
			"source",
			"private registration source",
			0,
			readsource.Registration([]passbooking.ReadAuthority{authority}),
		),
	)
	page, err := s.Read(t.Context(), "alice", conversation.Query{Limit: 1})
	require.NoError(t, err)
	require.NoError(
		t,
		s.CommitSummary(t.Context(), "alice", 0, []int64{page.Events[0].ID}, "private summarized source"),
	)
	window, err := s.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	require.Equal(t, readsource.Registration([]passbooking.ReadAuthority{authority}), window.Summary.ReadAuthorities)
	require.NoError(
		t,
		s.AppendDerived(t.Context(), "alice", "inherited", "private inferred reply", 0, window.Summary.ReadAuthorities),
	)
	require.NoError(t, s.AppendOriginal(t.Context(), "alice", "recent", "user", "unrelated original"))
	_, err = s.DB.Exec(t.Context(), `DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'`)
	require.NoError(t, err)
	window, err = s.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	assert.Empty(t, window.Summary.Text)
	assert.EqualValues(t, 1, window.Generation)
	page, err = s.Read(t.Context(), "alice", conversation.Query{Limit: 3})
	require.NoError(t, err)
	assert.True(t, page.Events[1].Omitted)
	assert.True(t, page.Events[2].Omitted)
	assert.Equal(t, "unrelated original", page.Events[0].Text)
	require.Error(t, s.CommitSummary(t.Context(), "alice", 1, []int64{page.Events[2].ID}, "private stale summary"))
}

func TestConversationAuthorityArchiveWaitsForBookingDeletion(t *testing.T) {
	t.Parallel()
	s, authority := conversationAuthorityFixture(t)
	tx, err := s.DB.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = tx.Exec(t.Context(), `DELETE FROM core.pass_bookings WHERE event_id='dance' AND owner='alice'`)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- s.AppendDerived(t.Context(), "alice", "racing", "private late reply", 0, readsource.Registration([]passbooking.ReadAuthority{authority}))
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := s.DB.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%LockReadBooking%')`).
			Scan(&waiting)
		return queryErr == nil && waiting
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(t.Context()))
	requireCode(t, <-done, "history_stale")
	var count int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key='racing'`).
			Scan(&count),
	)
	assert.Zero(t, count)
}
