package integration_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestKnowledgeHTTPNativeRequiresEditorButAgentAccessRemainsAvailable(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	ordinary := knowledgeHTTPScope(t, f.h, f.other)
	content := knowledgeContent()
	article := knowledgeApply(t, knowledgeFixture{h: f.h}, f.reviewerScope, db.KnowledgeChange{
		Kind: "edit", ArticleID: uuid.New(), Content: &content, Reason: "Publish editor-access reference",
	}).Article
	require.NotNil(t, article)
	methods := []string{"overview", "capabilities", "list", "search", "read", "revisions", "suggestion",
		"my_suggestions", "pending_suggestions", "moderators", "recent_actions"}
	for _, kind := range []string{"suggest", "edit", "review", "set_moderator"} {
		for _, phase := range []string{"prepare", "confirm", "status"} {
			methods = append(methods, kind+"/"+phase)
		}
	}
	for _, method := range methods {
		// The editor gate runs before argument parsing or operation recovery.
		knowledgeNativeCall(t, f, f.other, method, struct{}{}, http.StatusForbidden)
	}
	read := knowledgeAgentCall(t, f, ordinary, "read", map[string]any{
		"protocol_version": 1, "operation_id": uuid.NewString(),
		"arguments": map[string]any{"article_id": article.ID},
	}, http.StatusOK)
	require.Contains(t, string(read), article.ID.String())
	op := uuid.NewString()
	prep := knowledgeHTTPPrepare(t, knowledgeAgentCall(t, f, ordinary, "suggest/prepare", map[string]any{
		"protocol_version": 1, "operation_id": op,
		"arguments": map[string]any{"article_id": article.ID, "expected_revision": 1,
			"content": content, "reason": "Ordinary user works through the agent"},
	}, http.StatusOK))
	confirmed := knowledgeAgentCall(t, f, ordinary, "suggest", map[string]any{
		"protocol_version": 1, "operation_id": op, "arguments": prep.Arguments,
		"idempotency_key": op,
		"confirmation":    map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions},
	}, http.StatusOK)
	require.Contains(t, string(confirmed), `"status":"succeeded"`)
	knowledgeNativeCall(t, f, f.other, "suggest/status", map[string]any{"operation_id": op}, http.StatusForbidden)
	knowledgeNativeCall(t, f, f.reviewer, "read", map[string]any{"article_id": article.ID}, http.StatusOK)
}
