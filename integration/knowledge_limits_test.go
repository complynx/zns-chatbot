package integration_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func TestKnowledgeMaximumContentCanBeSuggestedApprovedAndReplayed(t *testing.T) {
	t.Parallel()
	for _, escaped := range []bool{false, true} {
		name := "plain"
		if escaped {
			name = "json_escaped"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeFixture(t)
			content := knowledgeContent()
			content.Body = strings.Repeat("a", knowledge.MaxBodyBytes)
			tail, label := "a", strings.Repeat("L", knowledge.MaxTitleRunes)
			if escaped {
				content.Body = "x" + strings.Repeat("\x01", knowledge.MaxBodyBytes-1)
				tail, label = "\\", strings.Repeat("😀", knowledge.MaxTitleRunes)
			}
			const prefix = "https://example.test/?q="
			for range knowledge.MaxSources {
				content.Sources = append(
					content.Sources,
					knowledge.Source{
						Label: label,
						URL:   prefix + strings.Repeat(tail, knowledge.MaxSourceURLBytes-len(prefix)),
					},
				)
			}
			normalized, err := content.Normalize()
			require.NoError(t, err)
			proposed := knowledgeApply(
				t,
				f,
				f.user,
				db.KnowledgeChange{
					Kind:      "suggest",
					ArticleID: uuid.New(),
					Content:   &normalized,
					Reason:    "A supported maximum-size reference",
				},
			)
			require.NotNil(t, proposed.Suggestion)
			op := uuid.NewString()
			review := db.KnowledgeChange{
				Kind:         "review",
				SuggestionID: proposed.Suggestion.ID,
				Decision:     "approve",
				Reason:       "Checked the complete supported content",
			}
			prep, err := f.h.store.PrepareKnowledge(t.Context(), f.moderator, op, review)
			require.NoError(t, err)
			result, err := f.h.store.ConfirmKnowledge(t.Context(), f.moderator, op, prep.Arguments, prep.ID)
			require.NoError(t, err)
			require.NotNil(t, result.Article)
			current, err := f.h.store.ReadKnowledge(t.Context(), f.user, result.Article.ID, 0)
			require.NoError(t, err)
			require.Equal(t, normalized, current.Content)
			reopened, err := db.Open(t.Context(), f.h.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			replay, err := reopened.ConfirmKnowledge(t.Context(), f.moderator, op, prep.Arguments, prep.ID)
			require.NoError(t, err)
			require.JSONEq(t, knowledgeReceiptJSON(t, result), knowledgeReceiptJSON(t, replay))
		})
	}
}
