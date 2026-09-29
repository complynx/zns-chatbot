package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

type classifierModel struct {
	agent.Scripted

	calls int
}

func (m *classifierModel) AssessKnowledge(
	ctx context.Context,
	_ agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	m.calls++
	return agent.KnowledgeAssessment{Worthwhile: true, Reason: "Useful fact"}, ctx.Err()
}

func TestObservedModelPreservesKnowledgeClassifier(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	next := &classifierModel{}
	model := observedModel{next: next, runtime: runtime}
	verdict, err := model.AssessKnowledge(t.Context(), agent.KnowledgeAssessmentInput{Text: "Venue detail"})
	require.NoError(t, err)
	assert.True(t, verdict.Worthwhile)
	assert.Equal(t, 1, next.calls)
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(
		t,
		response.Body.String(),
		`zns_operations_total{operation="model.knowledge_assessment",result="ok"} 1`,
	)
	model.next = agent.Scripted{}
	_, err = model.AssessKnowledge(t.Context(), agent.KnowledgeAssessmentInput{Text: "Venue detail"})
	require.Error(t, err)
}
