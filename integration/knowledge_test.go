package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

type knowledgeFixture struct {
	h                                         *harness
	admin, moderator, user, other, eventAdmin db.PaymentScope
}

func newKnowledgeFixture(t *testing.T) knowledgeFixture {
	t.Helper()
	h := prepareHarness(t, newPeers(t))
	actors := make([]db.PaymentScope, 5)
	for i := range actors {
		id := int64(41001 + i)
		accepted, _, err := h.store.Intake(
			t.Context(),
			db.IntakeRequest{BotID: 123, TelegramUserID: id, ChatID: id, UpdateID: id, Payload: json.RawMessage(`{}`)},
			nil,
		)
		require.NoError(t, err)
		subject := "knowledge-" + accepted.UserID.String()
		_, err = h.store.BindSubject(t.Context(), accepted.UserID, subject, "org")
		require.NoError(t, err)
		actors[i] = db.PaymentScope{
			BotID: 123, Organization: "org",
			UserID:  accepted.UserID,
			Subject: subject,
		}
	}
	actors[0].GlobalAdmin = true
	f := knowledgeFixture{
		h:          h,
		admin:      actors[0],
		moderator:  actors[1],
		user:       actors[2],
		other:      actors[3],
		eventAdmin: actors[4],
	}
	enabled := true
	knowledgeApply(
		t,
		f,
		f.admin,
		db.KnowledgeChange{
			Kind:             "set_moderator",
			ModeratorUserID:  f.moderator.UserID,
			ModeratorEnabled: &enabled,
			Reason:           "Assign the test knowledge moderator",
		},
	)
	return f
}

func knowledgeApply(
	t *testing.T,
	f knowledgeFixture,
	actor db.PaymentScope,
	change db.KnowledgeChange,
) db.KnowledgeResult {
	t.Helper()
	op := uuid.NewString()
	prep, err := f.h.store.PrepareKnowledge(t.Context(), actor, op, change)
	require.NoError(t, err)
	result, err := f.h.store.ConfirmKnowledge(t.Context(), actor, op, prep.Arguments, prep.ID)
	require.NoError(t, err)
	return result
}

func knowledgeContent() knowledge.Content {
	return knowledge.Content{
		Slug:     "zouk-faq",
		Title:    "Zouk reference",
		Language: "en",
		Topics:   []string{"zouk"},
		Body:     "# Rules\nPublished community reference.",
		Sources:  []knowledge.Source{},
	}
}

func TestKnowledgeSuggestionRequiresDistinctModeratorAndReplaysOriginalResult(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	content := knowledgeContent()
	change := db.KnowledgeChange{
		Kind:      "suggest",
		ArticleID: uuid.New(),
		Content:   &content,
		Reason:    "Add a useful reference",
	}
	submitted := knowledgeApply(t, f, f.user, change)
	require.NotNil(t, submitted.Suggestion)
	suggestion := submitted.Suggestion
	require.Equal(t, "pending", suggestion.State)
	page, err := f.h.store.SearchKnowledge(
		t.Context(),
		f.user,
		knowledge.SearchRequest{Collection: knowledge.General, Queries: []string{"zouk"}},
	)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	_, err = f.h.store.ReadKnowledgeSuggestion(t.Context(), f.other, suggestion.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	review := db.KnowledgeChange{
		Kind:         "review",
		SuggestionID: suggestion.ID,
		Decision:     "approve",
		Reason:       "Checked the proposed source",
	}
	_, err = f.h.store.PrepareKnowledge(t.Context(), f.user, uuid.NewString(), review)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	// Becoming a moderator does not turn one's own suggestion into independent review.
	enabled := true
	knowledgeApply(
		t,
		f,
		f.admin,
		db.KnowledgeChange{
			Kind:             "set_moderator",
			ModeratorUserID:  f.user.UserID,
			ModeratorEnabled: &enabled,
			Reason:           "Grant moderator authority after submission",
		},
	)
	_, err = f.h.store.PrepareKnowledge(t.Context(), f.user, uuid.NewString(), review)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	op := uuid.NewString()
	prep, err := f.h.store.PrepareKnowledge(t.Context(), f.moderator, op, review)
	require.NoError(t, err)
	approved, err := f.h.store.ConfirmKnowledge(t.Context(), f.moderator, op, prep.Arguments, prep.ID)
	require.NoError(t, err)
	require.NotNil(t, approved.Article)
	require.Equal(t, change.ArticleID, approved.Article.ID)
	require.EqualValues(t, 1, approved.Article.Revision)
	reopened, err := db.Open(t.Context(), f.h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err := reopened.ConfirmKnowledge(t.Context(), f.moderator, op, prep.Arguments, prep.ID)
	require.NoError(t, err)
	require.JSONEq(t, knowledgeReceiptJSON(t, approved), knowledgeReceiptJSON(t, replay))
	receipt, err := reopened.KnowledgeOperation(t.Context(), f.moderator, "review", op)
	require.NoError(t, err)
	require.JSONEq(t, knowledgeReceiptJSON(t, approved), knowledgeReceiptJSON(t, receipt))
	page, err = reopened.SearchKnowledge(
		t.Context(),
		f.user,
		knowledge.SearchRequest{Collection: knowledge.General, Queries: []string{"zouk", "reference"}},
	)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	own, err := reopened.ReadKnowledgeSuggestion(t.Context(), f.user, suggestion.ID)
	require.NoError(t, err)
	require.Equal(t, "approved", own.State)
	require.NotNil(t, own.ReviewerID)
	require.Equal(t, f.moderator.UserID, *own.ReviewerID)
}

func TestKnowledgeEndedEventMovesDiscoveryAndAuthorityBeforeSweep(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	event, err := f.h.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.EventChange{
			ID: uuid.New(),
			Document: events.Document{
				Name:     "Knowledge event",
				Timezone: "Europe/Minsk",
				StartsAt: now.Add(-48 * time.Hour),
				EndsAt:   now.Add(time.Hour),
				Modules:  []string{},
			},
		},
	)
	require.NoError(t, err)
	_, err = f.h.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.AuthorityChange{EventID: event.ID, UserID: f.eventAdmin.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	content := knowledgeContent()
	content.Title = "Venue"
	content.Body = "# Venue\nOriginal event directions."
	created := knowledgeApply(
		t,
		f,
		f.eventAdmin,
		db.KnowledgeChange{
			Kind:      "edit",
			ArticleID: uuid.New(),
			EventID:   &event.ID,
			Content:   &content,
			Reason:    "Publish venue directions",
		},
	)
	require.NotNil(t, created.Article)
	stale := db.KnowledgeChange{
		Kind:             "edit",
		ArticleID:        created.Article.ID,
		ExpectedRevision: 1,
		EventID:          &event.ID,
		Content:          &content,
		Reason:           "An event-admin change prepared before closure",
	}
	op := uuid.NewString()
	prep, err := f.h.store.PrepareKnowledge(t.Context(), f.eventAdmin, op, stale)
	require.NoError(t, err)
	ended := event.Document
	ended.EndsAt = now.Add(-time.Second)
	_, err = f.h.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: ended},
	)
	require.NoError(t, err)
	page, err := f.h.store.SearchKnowledge(
		t.Context(),
		f.user,
		knowledge.SearchRequest{Collection: knowledge.General, Queries: []string{"venue"}},
	)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.True(t, page.Items[0].Article.Historical)
	active, err := f.h.store.SearchKnowledge(
		t.Context(),
		f.user,
		knowledge.SearchRequest{Collection: knowledge.Event, EventID: &event.ID, Queries: []string{"venue"}},
	)
	require.NoError(t, err)
	require.Empty(t, active.Items)
	_, err = f.h.store.ConfirmKnowledge(t.Context(), f.eventAdmin, op, prep.Arguments, prep.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	_, err = f.h.store.ArchiveEndedKnowledge(t.Context(), f.admin.EventScope, uuid.Nil)
	require.NoError(t, err)
	archived, err := f.h.store.ReadKnowledge(t.Context(), f.user, created.Article.ID, 0)
	require.NoError(t, err)
	require.True(t, archived.Historical)
	require.Equal(t, &event.ID, archived.EventID)
	require.Equal(t, content.Body, archived.Content.Body)
	require.NotNil(t, archived.ArchivedAt)
	require.EqualValues(t, 2, archived.Revision)
	_, err = f.h.store.ArchiveEndedKnowledge(t.Context(), f.admin.EventScope, uuid.Nil)
	require.NoError(t, err)
	again, err := f.h.store.ReadKnowledge(t.Context(), f.user, created.Article.ID, 0)
	require.NoError(t, err)
	require.Equal(t, archived.Revision, again.Revision)
	old, err := f.h.store.ReadKnowledge(t.Context(), f.user, created.Article.ID, 1)
	require.NoError(t, err)
	require.Equal(t, content.Body, old.Content.Body)
	corrected := content
	corrected.Body += "\nHistorical correction."
	result := knowledgeApply(
		t,
		f,
		f.moderator,
		db.KnowledgeChange{
			Kind:             "edit",
			ArticleID:        archived.ID,
			ExpectedRevision: archived.Revision,
			EventID:          &event.ID,
			Content:          &corrected,
			Reason:           "Correct the historical article",
		},
	)
	require.EqualValues(t, 3, result.Article.Revision)
}

func TestKnowledgeRevokedModeratorCannotCommitPreparedEdit(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	content := knowledgeContent()
	created := knowledgeApply(
		t,
		f,
		f.moderator,
		db.KnowledgeChange{Kind: "edit", ArticleID: uuid.New(), Content: &content, Reason: "Initial reference"},
	)
	changed := content
	changed.Body = "This prepared edit must not publish after revocation."
	op := uuid.NewString()
	prep, err := f.h.store.PrepareKnowledge(
		t.Context(),
		f.moderator,
		op,
		db.KnowledgeChange{
			Kind:             "edit",
			ArticleID:        created.Article.ID,
			ExpectedRevision: 1,
			Content:          &changed,
			Reason:           "Proposed correction",
		},
	)
	require.NoError(t, err)
	disabled := false
	knowledgeApply(
		t,
		f,
		f.admin,
		db.KnowledgeChange{
			Kind:                     "set_moderator",
			ModeratorUserID:          f.moderator.UserID,
			ModeratorEnabled:         &disabled,
			ExpectedModeratorVersion: 1,
			Reason:                   "Revoke the moderator",
		},
	)
	_, err = f.h.store.ConfirmKnowledge(t.Context(), f.moderator, op, prep.Arguments, prep.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	current, err := f.h.store.ReadKnowledge(t.Context(), f.user, created.Article.ID, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, current.Revision)
	require.Equal(t, content.Body, current.Content.Body)
}

func TestKnowledgePendingSuggestionSurvivesArchiveWithFreshHistoricalReview(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	event, err := f.h.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.EventChange{
			ID: uuid.New(),
			Document: events.Document{
				Name:     "Pending knowledge event",
				Timezone: "Europe/Minsk",
				StartsAt: now.Add(-48 * time.Hour),
				EndsAt:   now.Add(time.Hour),
				Modules:  []string{},
			},
		},
	)
	require.NoError(t, err)
	_, err = f.h.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.AuthorityChange{EventID: event.ID, UserID: f.eventAdmin.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	content := knowledgeContent()
	created := knowledgeApply(
		t,
		f,
		f.eventAdmin,
		db.KnowledgeChange{
			Kind:      "edit",
			ArticleID: uuid.New(),
			EventID:   &event.ID,
			Content:   &content,
			Reason:    "Publish event knowledge",
		},
	)
	proposed := content
	proposed.Body += "\nAn independently proposed correction."
	submitted := knowledgeApply(
		t,
		f,
		f.user,
		db.KnowledgeChange{
			Kind:             "suggest",
			ArticleID:        created.Article.ID,
			ExpectedRevision: 1,
			EventID:          &event.ID,
			Content:          &proposed,
			Reason:           "Correct the source material",
		},
	)
	ended := event.Document
	ended.EndsAt = now.Add(-time.Second)
	_, err = f.h.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: ended},
	)
	require.NoError(t, err)
	_, err = f.h.store.ArchiveEndedKnowledge(t.Context(), f.admin.EventScope, uuid.Nil)
	require.NoError(t, err)
	review := db.KnowledgeChange{
		Kind:             "review",
		SuggestionID:     submitted.Suggestion.ID,
		ExpectedRevision: 2,
		Decision:         "approve",
		Reason:           "Review the original correction as historical material",
	}
	_, err = f.h.store.PrepareKnowledge(t.Context(), f.eventAdmin, uuid.NewString(), review)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	approved := knowledgeApply(t, f, f.moderator, review)
	require.NotNil(t, approved.Article)
	require.Equal(t, created.Article.ID, approved.Article.ID)
	require.EqualValues(t, 3, approved.Article.Revision)
	require.Equal(t, proposed.Body, approved.Article.Content.Body)
	require.True(t, approved.Article.Historical)
	original, err := f.h.store.ReadKnowledgeSuggestion(t.Context(), f.user, submitted.Suggestion.ID)
	require.NoError(t, err)
	require.Equal(t, submitted.Suggestion.ID, original.ID)
	require.EqualValues(t, 1, original.BaseRevision)
	require.Equal(t, "approved", original.State)
	require.Equal(t, f.moderator.UserID, *original.ReviewerID)
}

func TestKnowledgeEventEditorCannotUseAnotherEventOrAmbassadorRole(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	var ids []uuid.UUID
	for range 2 {
		event, err := f.h.store.ChangeEvent(
			t.Context(),
			f.admin.EventScope,
			f.admin.Subject,
			uuid.NewString(),
			db.EventChange{
				ID: uuid.New(),
				Document: events.Document{
					Name:     "Knowledge authority event",
					Timezone: "Europe/Minsk",
					StartsAt: now,
					EndsAt:   now.Add(time.Hour),
					Modules:  []string{},
				},
			},
		)
		require.NoError(t, err)
		ids = append(ids, event.ID)
	}
	for _, assignment := range []db.AuthorityChange{{EventID: ids[0], UserID: f.eventAdmin.UserID, Role: db.AuthorityAdministrator}, {EventID: ids[0], UserID: f.other.UserID, Role: db.AuthorityAmbassador}} {
		_, err := f.h.store.ChangeAuthority(
			t.Context(),
			f.admin.EventScope,
			f.admin.Subject,
			uuid.NewString(),
			assignment,
			uuid.Nil,
		)
		require.NoError(t, err)
	}
	content := knowledgeContent()
	change := db.KnowledgeChange{
		Kind:      "edit",
		ArticleID: uuid.New(),
		EventID:   &ids[0],
		Content:   &content,
		Reason:    "Publish scoped event knowledge",
	}
	_, err := f.h.store.PrepareKnowledge(t.Context(), f.other, uuid.NewString(), change)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	change.EventID = &ids[1]
	_, err = f.h.store.PrepareKnowledge(t.Context(), f.eventAdmin, uuid.NewString(), change)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	change.EventID = nil
	_, err = f.h.store.PrepareKnowledge(t.Context(), f.admin, uuid.NewString(), change)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	change.EventID = &ids[0]
	result := knowledgeApply(t, f, f.eventAdmin, change)
	require.NotNil(t, result.Article)
	require.Equal(t, &ids[0], result.Article.EventID)
}

// Compare the public receipt, including timestamps, across its durable JSON round trip.
func knowledgeReceiptJSON(t *testing.T, result db.KnowledgeResult) string {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	return string(raw)
}
