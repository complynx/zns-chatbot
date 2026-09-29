package bot

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestKnowledgeAssessmentCarrierUsesHostProjection(t *testing.T) {
	t.Parallel()
	refs := []readsource.Authority{
		{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "host-only-review"}},
	}
	result := knowledge.Result{ReadAuthorities: refs}
	original := agenthost.ScriptKnowledgeAssessmentResult{
		Result: result,
		Assessment: interaction.KnowledgeAssessmentState{
			Status: interaction.KnowledgeAssessmentDeferred,
			Reason: interaction.KnowledgeDeferredUnavailable,
		},
	}
	projected, ok := agenthost.ModelToolEvidence(original).(agenthost.ScriptKnowledgeAssessmentResult)
	require.True(t, ok)
	require.Equal(t, original.Assessment, projected.Assessment)
	require.Empty(t, projected.ReadAuthorities)
	require.Equal(t, refs, original.ReadAuthorities, "host capture must keep its original authority")
	raw, err := json.Marshal(projected)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "host-only-review")
	require.NotContains(t, string(raw), "read_authorities")
}

func TestKnowledgeProjectionPreservesBodiesAndHostEvidence(t *testing.T) {
	t.Parallel()
	refs := []readsource.Authority{
		{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "review-only"}},
	}
	body := strings.Repeat("private body ", 300)
	original := knowledge.MemoryPage{
		Entries:         []knowledge.MemoryEntry{{Text: body, ReadAuthorities: refs}},
		ReadAuthorities: refs,
	}
	projected, ok := agenthost.ModelToolEvidence(original).(knowledge.MemoryPage)
	require.True(t, ok)
	require.Equal(t, body, projected.Entries[0].Text)
	require.Empty(t, projected.ReadAuthorities)
	require.Empty(t, projected.Entries[0].ReadAuthorities)
	require.Equal(t, refs, original.ReadAuthorities)
	require.Equal(t, refs, original.Entries[0].ReadAuthorities)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "read_authorities")
	require.NotContains(t, string(encoded), "review-only")

	page := conversation.Page{ReadAuthorities: refs, Events: []conversation.Event{{Text: body}}}
	captured, err := agenthost.HistoryPageReadAuthorities(page)
	require.NoError(t, err)
	require.Equal(t, refs, captured)
	visible, ok := agenthost.ModelToolEvidence(page).(conversation.Page)
	require.True(t, ok)
	require.Empty(t, visible.ReadAuthorities)
	require.Equal(t, body, visible.Events[0].Text)
	require.Equal(t, refs, page.ReadAuthorities)
}

func TestScriptSourceFailureDistinguishesRetryableOutage(t *testing.T) {
	t.Parallel()
	require.JSONEq(
		t,
		`{"error":"stale","restart":true}`,
		string(agenthost.ScriptSourceFailure(appclient.ErrReadStale, appclient.ErrReadStale)),
	)
	require.JSONEq(
		t,
		`{"error":"source_authority_limit","new_turn_required":true}`,
		string(agenthost.ScriptSourceFailure(readsource.ErrLimit, appclient.ErrReadStale)),
	)
	require.Nil(t, agenthost.ScriptSourceFailure(errors.New("database unavailable"), appclient.ErrReadStale))
	require.Nil(
		t,
		agenthost.ScriptSourceFailure(&core.ProblemError{Status: 503, Code: "internal_error"}, appclient.ErrReadStale),
	)
}
