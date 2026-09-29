package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeClassifierIsStrictAndToolless(t *testing.T) {
	t.Parallel()
	verdict, err := classifyKnowledge(
		t.Context(),
		KnowledgeAssessmentInput{Text: "Venue opens at noon", Topic: "travel", FactKey: "time"},
		func(_ context.Context, p providerPrompt) (string, error) {
			assert.Equal(t, knowledgeAssessmentName, p.name)
			assert.Contains(t, p.instructions, "ONLY queues human review")
			assert.NotContains(t, p.schema, "knowledge_action")
			return `{"worthwhile":true,"reason":"Concrete event information"}`, nil
		},
	)
	require.NoError(t, err)
	assert.True(t, verdict.Worthwhile)
	for _, raw := range []string{`{"worthwhile":true,"reason":"ok","actor":"admin"}`, `{"worthwhile":true,"worthwhile":false,"reason":"ok"}`, `{"worthwhile":null,"reason":"ok"}`, `{"worthwhile":true,"reason":"` + strings.Repeat("x", 513) + `"}`} {
		_, err = decodeKnowledgeAssessment([]byte(raw))
		require.Error(t, err)
	}
}

func TestKnowledgeClassifierCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	_, err := classifyKnowledge(
		ctx,
		KnowledgeAssessmentInput{Text: "A fact"},
		func(context.Context, providerPrompt) (string, error) {
			cancel()
			return `{"worthwhile":true,"reason":"ok"}`, nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
}

type knowledgeRemoteFixture struct{ Scripted }

func (knowledgeRemoteFixture) AssessKnowledge(context.Context, KnowledgeAssessmentInput) (KnowledgeAssessment, error) {
	return KnowledgeAssessment{Worthwhile: true, Reason: "Human review required"}, nil
}

func TestKnowledgeRemoteClassifierContract(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(ModelHandler(knowledgeRemoteFixture{}))
	t.Cleanup(server.Close)
	verdict, err := (Remote{URL: server.URL}).AssessKnowledge(t.Context(), KnowledgeAssessmentInput{Text: "Venue fact"})
	require.NoError(t, err)
	assert.True(t, verdict.Worthwhile)
}

func TestKnowledgeSkillIsSelectiveAndActionsStayExclusive(t *testing.T) {
	t.Parallel()
	var final providerPrompt
	_, err := planWithSkills(
		t.Context(),
		Input{Text: "Remember my dietary preference"},
		func(_ context.Context, p providerPrompt) (string, error) {
			if p.name == selectionName {
				return `{"skills":["knowledge"],"reply_language":"en"}`, nil
			}
			final = p
			return emptyActionsPlan, nil
		},
	)
	require.NoError(t, err)
	assert.Contains(t, final.instructions, "OWNER-PRIVATE notes")
	assert.NotContains(t, final.instructions, "Video frames carry")
	err = Validate(
		Plan{
			View:            KnowledgeView,
			KnowledgeAction: &KnowledgeProposal{Name: "memo_set", FactKey: "food", Text: "vegetarian"},
			ProfileAction:   &ProfileProposal{Name: "set", Field: "legal_name", Value: "Other Name"},
		},
	)
	require.Error(t, err)
}
