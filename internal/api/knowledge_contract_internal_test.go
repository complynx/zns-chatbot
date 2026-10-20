package api

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func knowledgeValidate(t *testing.T, schema any, value any, mode huma.ValidateMode) []error {
	t.Helper()
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	var parsed huma.Schema
	require.NoError(t, json.Unmarshal(encoded, &parsed))
	parsed.PrecomputeMessages()
	encoded, err = json.Marshal(value)
	require.NoError(t, err)
	var input any
	require.NoError(t, json.Unmarshal(encoded, &input))
	result := &huma.ValidateResult{}
	huma.Validate(
		huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer),
		&parsed,
		huma.NewPathBuffer(nil, 0),
		mode,
		input,
		result,
	)
	return result.Errors
}
func TestKnowledgeDiscoveryUsesCurrentCapabilities(t *testing.T) {
	t.Parallel()
	ids := func(c db.KnowledgeCapabilities) map[string]map[string]any {
		found := map[string]map[string]any{}
		for _, method := range knowledgeMethods(c) {
			found[method["id"].(string)] = method
		}
		return found
	}
	ordinary := ids(db.KnowledgeCapabilities{})
	require.Contains(t, ordinary, "search")
	require.Contains(t, ordinary, "suggest")
	require.Contains(t, ordinary, "my_suggestions")
	require.NotContains(t, ordinary, "pending_suggestions")
	require.NotContains(t, ordinary, "edit")
	require.NotContains(t, ordinary, "review")
	require.NotContains(t, ordinary, "set_moderator")
	reviewer := ids(db.KnowledgeCapabilities{CanEdit: true, Moderator: true})
	require.Contains(t, reviewer, "pending_suggestions")
	require.Contains(t, reviewer, "edit")
	require.Contains(t, reviewer, "review")
	require.NotContains(t, reviewer, "set_moderator")
	global := ids(db.KnowledgeCapabilities{GlobalAdministrator: true})
	require.Contains(t, global, "set_moderator")
	require.Contains(t, global, "moderators")
	require.NotContains(t, global, "review")
	module := knowledgeModule()
	require.Equal(t, 0, module["cache_ttl_seconds"])
	for _, kind := range []string{"suggest", "edit", "review", "set_moderator"} {
		method := ids(db.KnowledgeCapabilities{CanEdit: true, GlobalAdministrator: true})[kind]
		require.Equal(t, effectWrite, method[manifestEffect])
		require.Equal(t, true, method[manifestIdempotency])
		require.Equal(t, knowledgePath+kind+"/status", method[manifestStatusPath])
		require.Equal(
			t,
			map[string]any{
				confirmationMode:    assignmentConfirmationMode,
				manifestPreparePath: knowledgePath + kind + "/prepare",
			},
			method[manifestConfirmation],
		)
	}
}
func TestKnowledgeNormalizedPreparationMatchesVerbInput(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	content, err := (knowledge.Content{Slug: " ZOUK ", Title: " Zouk ", Language: " EN ", Topics: []string{" Dance "}, Body: "# Source\nExact source", Sources: []knowledge.Source{}}).Normalize()
	require.NoError(t, err)
	change := db.KnowledgeChange{Kind: "suggest", Content: &content, Reason: " Add reference ", ExpectedRevision: 0}
	args := knowledgeEditFromChange(change)
	require.Empty(t, knowledgeValidate(t, knowledgeSchema[knowledgeEditArguments](), args, huma.ModeWriteToServer))
	encoded, err := json.Marshal(args)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"kind"`)
	require.NotContains(t, string(encoded), `"suggestion_id"`)
	require.NotContains(t, string(encoded), `"moderator_enabled"`)
	require.Equal(t, content, *knowledgeEditChange(args).Content)
	review := knowledgeReviewFromChange(
		db.KnowledgeChange{
			Kind:             "review",
			SuggestionID:     id,
			Decision:         "approve",
			Reason:           "Checked sources",
			ExpectedRevision: 0,
		},
	)
	require.Empty(t, knowledgeValidate(t, knowledgeSchema[knowledgeReviewArguments](), review, huma.ModeWriteToServer))
	wrong := map[string]any{"suggestion_id": id.String(), "decision": "approve", "reason": "checked"}
	require.NotEmpty(
		t,
		knowledgeValidate(t, knowledgeSchema[knowledgeReviewArguments](), wrong, huma.ModeWriteToServer),
		"the current article revision is required even when zero",
	)
	disabled := knowledgeModeratorFromChange(
		db.KnowledgeChange{
			ModeratorUserID:          id,
			ModeratorEnabled:         new(false),
			Reason:                   "Revoke",
			ExpectedModeratorVersion: 0,
		},
	)
	require.Empty(
		t,
		knowledgeValidate(t, knowledgeSchema[knowledgeModeratorArguments](), disabled, huma.ModeWriteToServer),
	)
	require.NotEmpty(
		t,
		knowledgeValidate(
			t,
			knowledgeSchema[knowledgeModeratorArguments](),
			map[string]any{"moderator_user_id": id, "reason": "revoke", "expected_moderator_version": 0},
			huma.ModeWriteToServer,
		),
		"omission must not revoke",
	)
}
func TestKnowledgeProjectionBoundsAndImmutableReferences(t *testing.T) {
	t.Parallel()
	event := uuid.New()
	id := uuid.New()
	body := "# Источник\n" + strings.Repeat("🙂 точный текст <>& ", 2000)
	article := db.KnowledgeArticle{
		ID:       id,
		Revision: 7,
		Content: knowledge.Content{
			Slug:     "reference",
			Title:    "Reference",
			Language: "ru",
			Body:     body,
			Topics:   []string{"a", "b", "c", "d", "e"},
			Sources: []knowledge.Source{
				{Label: "one", URL: "https://example.org/one"},
				{Label: "two", URL: "https://example.org/two"},
				{Label: "three", URL: "https://example.org/three"},
			},
		},
		EventID:    &event,
		EventName:  "Past event",
		Historical: true,
		UpdatedAt:  time.Now(),
	}
	view := knowledgeProject(article, true)
	require.Equal(t, id, view.ID)
	require.Equal(t, int64(7), view.Revision)
	require.Equal(t, event, *view.EventID)
	require.True(t, view.Historical)
	require.Equal(t, knowledge.General, view.Collection)
	require.Len(t, view.Topics, 4)
	require.Len(t, view.Sources, 2)
	require.True(t, view.MetadataTruncated)
	require.LessOrEqual(t, len(view.Excerpt.Text), 256)
	require.NotNil(t, view.Excerpt.NextByte)
	require.Equal(t, body[view.Excerpt.StartByte:view.Excerpt.EndByte], view.Excerpt.Text)
	// The maximum article page contains previews, never twenty complete sources.
	page := knowledgeListView{
		Items:   make([]knowledgeArticleView, 20),
		Filters: db.KnowledgeListRequest{Collection: knowledge.General, Limit: 20},
	}
	for i := range page.Items {
		page.Items[i] = view
	}
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.Less(t, len(encoded), knowledgeResultLimit)
	require.NotContains(t, string(encoded), `"body"`)
	require.Empty(t, knowledgeValidate(t, knowledgeSchema[knowledgeListView](), page, huma.ModeReadFromServer))
	require.Empty(t, knowledgeValidate(t, knowledgeSchema[knowledgeArticleView](), view, huma.ModeReadFromServer))
}
func TestKnowledgeWritesDistinguishRejectionAndUncertainty(t *testing.T) {
	t.Parallel()
	for _, err := range []error{db.ErrConflict, db.ErrKnowledgeDenied, knowledge.ErrInvalid, pgx.ErrNoRows} {
		out, responseErr := knowledgeWriteResult(uuid.NewString(), db.KnowledgeResult{}, err)
		require.NoError(t, responseErr)
		require.Equal(t, agentRejected, out.Body.Status)
		require.Equal(t, agentTerminalRejection, out.Body.Error.Classification)
	}
	out, err := knowledgeWriteResult(uuid.NewString(), db.KnowledgeResult{}, errors.New("connection lost after commit"))
	require.Nil(t, out)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, 503, statusErr.GetStatus())
	missing, err := paymentStatusResult[knowledgeResultView](uuid.NewString(), nil, pgx.ErrNoRows)
	require.NoError(t, err)
	require.Equal(t, agentNotFound, missing.Body.Status)
	require.Error(t, knowledgeOperationID("new-operation"))
	require.Error(t, knowledgeOperationID(uuid.Nil.String()))
	require.NoError(t, knowledgeOperationID(uuid.NewString()))
}

func TestKnowledgeSearchBoundsAndPassageProvenance(t *testing.T) {
	t.Parallel()
	schema := knowledgeInputSchema[knowledge.SearchRequest]()
	require.Empty(
		t,
		knowledgeValidate(
			t,
			schema,
			map[string]any{"collection": "event", "queries": []string{"зук", "zouk"}, "limit": 20},
			huma.ModeWriteToServer,
		),
	)
	for _, invalid := range []map[string]any{
		{"queries": []string{}},
		{"queries": []string{"one", "two", "three", "four"}},
		{"queries": []string{"one"}, "collection": "secret"},
		{"queries": []string{"one"}, "limit": 21},
		{"queries": []string{"one"}, "actor_id": uuid.NewString()},
	} {
		require.NotEmpty(t, knowledgeValidate(t, schema, invalid, huma.ModeWriteToServer))
	}
	require.NotEmpty(
		t,
		knowledgeValidate(
			t,
			knowledgeInputSchema[knowledgeReadArguments](),
			map[string]any{"article_id": uuid.NewString(), "max_bytes": 16385},
			huma.ModeWriteToServer,
		),
	)
	body := "# Lead\nOther material\n## History\nExact ЗУК reference"
	article := db.KnowledgeArticle{
		ID:              uuid.New(),
		Revision:        2,
		CurrentRevision: 3,
		Content:         knowledge.Content{Body: body},
	}
	view := knowledgeProject(article, true)
	knowledgeSearchExcerpt(&view, body, []string{"зук"})
	require.Equal(t, "literal_match", view.ExcerptKind)
	require.Equal(t, "history", view.ExcerptAnchor)
	require.True(t, view.OlderRevision)
	require.Equal(t, int64(2), view.Revision)
	require.Equal(t, int64(3), view.CurrentRevision)
	require.Equal(t, body[view.Excerpt.StartByte:view.Excerpt.EndByte], view.Excerpt.Text)
	fallback := knowledgeProject(article, true)
	knowledgeSearchExcerpt(&fallback, body, []string{"no literal occurrence"})
	require.Equal(t, "lead", fallback.ExcerptKind)
	require.Empty(t, fallback.ExcerptAnchor)
	suggestion := knowledgeSuggestionProject(
		db.KnowledgeSuggestion{
			ID:           uuid.New(),
			ArticleID:    article.ID,
			BaseRevision: 2,
			Content:      article.Content,
			State:        "pending",
		},
		true,
	)
	encoded, err := json.Marshal(suggestion.Content)
	require.NoError(t, err)
	require.NotContains(
		t,
		string(encoded),
		`"revision"`,
		"suggestion bytes must not claim to be published base-revision bytes",
	)
	require.Empty(
		t,
		knowledgeValidate(t, knowledgeSchema[knowledgeSuggestionView](), suggestion, huma.ModeReadFromServer),
	)
}

// Agent discovery must accept the same pinned article references as JSON decoding.
func TestKnowledgeArticleNavigationAcceptsPinnedReferences(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	for _, method := range knowledgeMethods(db.KnowledgeCapabilities{}) {
		name := method["id"].(string)
		if name != "read" && name != "outline" && name != "find" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args := map[string]any{"article_id": id, "revision": 1}
			if name == "find" {
				args["query"] = "reference"
			}
			require.Empty(t, knowledgeValidate(t, method[manifestInputSchema], args, huma.ModeWriteToServer))
			args["revision"] = -1
			require.NotEmpty(t, knowledgeValidate(t, method[manifestInputSchema], args, huma.ModeWriteToServer))
			delete(args, "article_id")
			args["revision"] = 1
			require.NotEmpty(t, knowledgeValidate(t, method[manifestInputSchema], args, huma.ModeWriteToServer))
		})
	}
}
