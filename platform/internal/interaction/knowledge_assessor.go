package interaction

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// KnowledgeAssessor is the provider boundary. Its adapter checks live source
// authority and request budgets at every actual provider attempt.
type KnowledgeAssessor interface {
	AssessKnowledge(context.Context, string, knowledge.Proposal) (KnowledgeAssessmentAttempt, error)
}

type KnowledgeAssessmentStatus string
type KnowledgeDeferredReason string

const (
	KnowledgeAssessmentCompleted  KnowledgeAssessmentStatus = "completed"
	KnowledgeAssessmentDeferred   KnowledgeAssessmentStatus = "deferred"
	KnowledgeDeferredUnavailable  KnowledgeDeferredReason   = "unavailable"
	KnowledgeDeferredBudget       KnowledgeDeferredReason   = "budget"
	KnowledgeDeferredUnconfigured KnowledgeDeferredReason   = "unconfigured"
)

// KnowledgeAssessmentState contains only bounded operational facts, no provider
// diagnostics, source text or classifier verdict.
type KnowledgeAssessmentState struct {
	Status KnowledgeAssessmentStatus `json:"status"`
	Reason KnowledgeDeferredReason   `json:"reason,omitempty"`
}

type KnowledgeAssessmentAttempt struct {
	Status  KnowledgeAssessmentStatus
	Verdict agent.KnowledgeAssessment
	Reason  KnowledgeDeferredReason
}

func (a KnowledgeAssessmentAttempt) valid() bool {
	switch a.Status {
	case KnowledgeAssessmentCompleted:
		return a.Reason == ""
	case KnowledgeAssessmentDeferred:
		return a.Verdict == (agent.KnowledgeAssessment{}) &&
			(a.Reason == KnowledgeDeferredUnavailable || a.Reason == KnowledgeDeferredBudget || a.Reason == KnowledgeDeferredUnconfigured)
	default:
		return false
	}
}
