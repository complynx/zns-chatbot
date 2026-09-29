package agenthost

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// KnowledgeAssessor adapts the trusted classifier. Each real request checks live
// source authority; provider failures expose only a bounded deferred reason.
type KnowledgeAssessor struct {
	Classifier agent.KnowledgeClassifier
	Authority  SourceAuthority
}

func (a KnowledgeAssessor) AssessKnowledge(
	ctx context.Context,
	owner string,
	proposal knowledge.Proposal,
) (interaction.KnowledgeAssessmentAttempt, error) {
	if err := ctx.Err(); err != nil {
		return interaction.KnowledgeAssessmentAttempt{}, err
	}
	if a.Authority == nil {
		return interaction.KnowledgeAssessmentAttempt{}, errors.New("knowledge assessment authority unavailable")
	}
	if err := a.Authority.CheckReadAuthorities(ctx, owner, proposal.ReadAuthorities); err != nil {
		return interaction.KnowledgeAssessmentAttempt{}, err
	}
	if a.Classifier == nil {
		return deferredKnowledgeAssessment(interaction.KnowledgeDeferredUnconfigured)
	}
	var guardErr error
	requests := 0
	budgetExceeded := false
	verdict, err := a.Classifier.AssessKnowledge(ctx, agent.KnowledgeAssessmentInput{
		Event: proposal.Event, Topic: proposal.Topic, FactKey: proposal.FactKey, Text: proposal.Text,
		BeforeProvider: func(requestCtx context.Context) error {
			guardErr = a.Authority.CheckReadAuthorities(requestCtx, owner, proposal.ReadAuthorities)
			if guardErr != nil {
				return guardErr
			}
			if requests >= 1 {
				budgetExceeded = true
				return errors.New("knowledge assessment request budget exceeded")
			}
			requests++
			return nil
		},
	})
	if ctx.Err() != nil {
		return interaction.KnowledgeAssessmentAttempt{}, assessmentBoundaryError(ctx.Err(), err)
	}
	if guardErr != nil {
		return interaction.KnowledgeAssessmentAttempt{}, assessmentBoundaryError(guardErr, err)
	}
	if errors.Is(err, context.Canceled) {
		return interaction.KnowledgeAssessmentAttempt{}, err
	}
	if budgetExceeded {
		return deferredKnowledgeAssessment(interaction.KnowledgeDeferredBudget)
	}
	if err != nil {
		return deferredKnowledgeAssessment(interaction.KnowledgeDeferredUnavailable)
	}
	return interaction.KnowledgeAssessmentAttempt{
		Status:  interaction.KnowledgeAssessmentCompleted,
		Verdict: verdict,
	}, nil
}

func deferredKnowledgeAssessment(
	reason interaction.KnowledgeDeferredReason,
) (interaction.KnowledgeAssessmentAttempt, error) {
	return interaction.KnowledgeAssessmentAttempt{Status: interaction.KnowledgeAssessmentDeferred, Reason: reason}, nil
}

// Keep the original denial plus a bounded accounting failure. Provider and
// recorder diagnostics are never copied into this authority error channel.
func assessmentBoundaryError(reason, providerErr error) error {
	if errors.Is(providerErr, credits.ErrAccounting) {
		return errors.Join(reason, credits.ErrAccounting)
	}
	return reason
}
