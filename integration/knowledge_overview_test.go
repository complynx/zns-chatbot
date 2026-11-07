package integration_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func knowledgeTestEvent(t *testing.T, f knowledgeFixture, scope db.EventScope, change db.EventChange) events.Event {
	t.Helper()
	event, err := f.h.store.ChangeEvent(t.Context(), scope, f.admin.Subject, uuid.NewString(), change)
	require.NoError(t, err)
	return event
}

func TestKnowledgeOverviewDiscoversEmptyEventsAcrossPages(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	empty, err := f.h.store.KnowledgeOverview(t.Context(), f.user)
	require.NoError(t, err)
	require.Len(t, empty.Items, 1)
	require.EqualValues(t, 0, empty.Items[0].Count)
	require.Equal(t, knowledge.General, empty.Items[0].Collection)
	now := time.Now().UTC().Truncate(time.Microsecond)
	doc := events.Document{
		Name:     "An empty event",
		Timezone: "Europe/Minsk",
		StartsAt: now.Add(time.Hour),
		EndsAt:   now.Add(48 * time.Hour),
		Modules:  []string{},
	}
	expected := make([]uuid.UUID, 0, 22)
	for range 22 {
		event := knowledgeTestEvent(t, f, f.admin.EventScope, db.EventChange{ID: uuid.New(), Document: doc})
		expected = append(expected, event.ID)
	}
	foreign := f.admin.EventScope
	foreign.Organization = "other-knowledge-organization"
	knowledgeTestEvent(t, f, foreign, db.EventChange{ID: uuid.New(), Document: doc})
	doc.StartsAt, doc.EndsAt = now.Add(-48*time.Hour), now.Add(-time.Hour)
	knowledgeTestEvent(t, f, f.admin.EventScope, db.EventChange{ID: uuid.New(), Document: doc})
	content := knowledgeContent()
	knowledgeApply(
		t,
		f,
		f.moderator,
		db.KnowledgeChange{Kind: "edit", ArticleID: uuid.New(), Content: &content, Reason: "A general reference"},
	)
	first, err := f.h.store.KnowledgeOverview(t.Context(), f.user)
	require.NoError(t, err)
	require.Len(t, first.Items, 21)
	require.NotNil(t, first.NextEvent)
	require.True(t, first.Truncated)
	second, err := f.h.store.KnowledgeOverviewAfter(t.Context(), f.user, *first.NextEvent)
	require.NoError(t, err)
	require.Len(t, second.Items, 3)
	require.Nil(t, second.NextEvent)
	require.False(t, second.Truncated)
	for _, page := range []db.KnowledgeOverview{first, second} {
		require.Equal(t, knowledge.General, page.Items[0].Collection)
		require.EqualValues(t, 1, page.Items[0].Count)
		require.Nil(t, page.Items[0].EventID)
	}
	var actual []uuid.UUID
	for _, item := range slices.Concat(first.Items[1:], second.Items[1:]) {
		require.Equal(t, knowledge.Event, item.Collection)
		require.NotNil(t, item.EventID)
		require.EqualValues(t, 0, item.Count)
		require.Equal(t, "An empty event", item.EventName)
		actual = append(actual, *item.EventID)
	}
	require.ElementsMatch(t, expected, actual)
}

func TestKnowledgeOverviewKeepsRescheduledArchiveHistorical(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := knowledgeTestEvent(
		t,
		f,
		f.admin.EventScope,
		db.EventChange{
			ID: uuid.New(),
			Document: events.Document{
				Name:     "Original event",
				Timezone: "Europe/Minsk",
				StartsAt: now.Add(time.Hour),
				EndsAt:   now.Add(48 * time.Hour),
				Modules:  []string{},
			},
		},
	)
	content := knowledgeContent()
	created := knowledgeApply(
		t,
		f,
		f.admin,
		db.KnowledgeChange{
			Kind:      "edit",
			ArticleID: uuid.New(),
			EventID:   &event.ID,
			Content:   &content,
			Reason:    "The original event reference",
		},
	)
	ended := event.Document
	ended.StartsAt, ended.EndsAt = now.Add(-48*time.Hour), now.Add(-time.Hour)
	event = knowledgeTestEvent(
		t,
		f,
		f.admin.EventScope,
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: ended},
	)
	_, err := f.h.store.ArchiveEndedKnowledge(t.Context(), f.admin.EventScope, uuid.Nil)
	require.NoError(t, err)
	future := event.Document
	future.Name = "Changed event dates"
	future.StartsAt, future.EndsAt = now.Add(time.Hour), now.Add(48*time.Hour)
	knowledgeTestEvent(
		t,
		f,
		f.admin.EventScope,
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: future},
	)
	overview, err := f.h.store.KnowledgeOverview(t.Context(), f.user)
	require.NoError(t, err)
	require.Len(t, overview.Items, 1)
	require.Equal(t, knowledge.General, overview.Items[0].Collection)
	require.EqualValues(t, 1, overview.Items[0].Count)
	current, err := f.h.store.ReadKnowledge(t.Context(), f.user, created.Article.ID, 0)
	require.NoError(t, err)
	require.True(t, current.Historical)
	require.Equal(t, "Original event", current.EventName)
	require.NotNil(t, current.EventEndsAt)
	require.True(t, current.EventEndsAt.Equal(ended.EndsAt))
}
