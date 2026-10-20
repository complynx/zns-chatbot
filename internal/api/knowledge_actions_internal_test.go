package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func TestKnowledgeActionPreparationHasExactVerbArguments(t *testing.T) {
	t.Parallel()
	content := knowledge.Content{Slug: "zouk", Title: "Zouk", Language: "en",
		Topics: []string{}, Body: "# Source\nReference", Sources: []knowledge.Source{}}
	enabled := true
	for _, kind := range []string{"suggest", "edit", "review", "set_moderator"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			change := db.KnowledgeChange{Kind: kind, ArticleID: uuid.New(), Content: &content,
				SuggestionID: uuid.New(), Decision: "approve", Reason: "Checked source",
				ModeratorUserID: uuid.New(), ModeratorEnabled: &enabled}
			action := knowledgeActionProject(db.KnowledgeActionView{Kind: kind,
				OperationID: uuid.NewString(), State: "prepared", ObservedAt: time.Now(),
				Preparation: &db.KnowledgePreparation{ID: uuid.New(), Arguments: change,
					Description: "Exact original terms", ExpiresAt: time.Now().Add(time.Minute)}})
			raw, err := json.Marshal(action.Preparation.Arguments)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, json.Unmarshal(raw, &actual))
			require.NotContains(t, actual, "kind")
			switch kind {
			case "suggest", "edit":
				require.Contains(t, actual, "content")
				require.NotContains(t, actual, "decision")
				require.NotContains(t, actual, "moderator_user_id")
			case "review":
				require.Contains(t, actual, "suggestion_id")
				require.NotContains(t, actual, "content")
			case "set_moderator":
				require.Equal(t, true, actual["moderator_enabled"])
				require.NotContains(t, actual, "content")
				require.NotContains(t, actual, "suggestion_id")
			}
			require.Empty(t, knowledgeValidate(t, knowledgeSchema[knowledgeActionView](),
				action, huma.ModeReadFromServer))
		})
	}
}

func TestKnowledgeCompletedActionProjectsBoundedReceipt(t *testing.T) {
	t.Parallel()
	content := knowledge.Content{Body: strings.Repeat("full-body-", 1000)}
	action := knowledgeActionProject(db.KnowledgeActionView{Kind: "edit",
		OperationID: uuid.NewString(), State: "completed", ObservedAt: time.Now(),
		Result: &db.KnowledgeResult{Kind: "edit", Article: &db.KnowledgeArticle{
			ID: uuid.New(), Content: content}}})
	require.NotNil(t, action.Result.Article)
	require.LessOrEqual(t, len(action.Result.Article.Excerpt.Text), knowledgePreviewBytes)
	raw, err := json.Marshal(action)
	require.NoError(t, err)
	require.NotContains(t, string(raw), content.Body)
	require.Nil(t, action.Preparation)
}

func TestKnowledgeOperationLookupPreservesOriginalKey(t *testing.T) {
	t.Parallel()
	const original = "A9F7D8A9-9B10-4BE1-BB86-598E37BD5B52"
	require.NoError(t, knowledgeOperationID(original))
	var args knowledgeOperationArguments
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"edit","operation_id":"`+original+`"}`), &args))
	require.Equal(t, original, args.OperationID)
}
