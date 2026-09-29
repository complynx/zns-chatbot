package interaction

import (
	"context"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (c KnowledgeCoordinator) assess(
	ctx context.Context,
	owner string,
	updateID int64,
	p knowledge.Proposal,
	script bool,
) (KnowledgeAssessmentState, error) {
	if p.State != "pending_filter" {
		return KnowledgeAssessmentState{}, nil
	}
	kind := "knowledge_assessment"
	if script {
		kind += ":" + strconv.FormatInt(p.ID, 10)
	}
	verdict, state, err := c.assessmentVerdict(ctx, owner, updateID, kind, p)
	if err != nil || state.Status == KnowledgeAssessmentDeferred {
		return state, err
	}
	_, err = c.Host.AssessMemoryProposal(ctx, owner, knowledge.Assessment{
		Key: "tg-assess-" + strconv.FormatInt(p.ID, 10), ProposalID: p.ID, Version: p.Version,
		Worthwhile: verdict.Worthwhile, Reason: verdict.Reason,
	})
	if err != nil {
		return KnowledgeAssessmentState{}, err
	}
	return KnowledgeAssessmentState{Status: KnowledgeAssessmentCompleted}, nil
}

func (c KnowledgeCoordinator) assessmentVerdict(
	ctx context.Context,
	owner string,
	updateID int64,
	kind string,
	p knowledge.Proposal,
) (agent.KnowledgeAssessment, KnowledgeAssessmentState, error) {
	verdict, found, err := c.Store.loadKnowledgeAssessment(ctx, owner, updateID, kind)
	if err != nil || found {
		return verdict, KnowledgeAssessmentState{}, err
	}
	if c.Assessor == nil {
		return verdict, KnowledgeAssessmentState{
			Status: KnowledgeAssessmentDeferred,
			Reason: KnowledgeDeferredUnconfigured,
		}, nil
	}
	attempt, err := c.Assessor.AssessKnowledge(ctx, owner, p)
	if ctx.Err() != nil {
		return verdict, KnowledgeAssessmentState{}, errors.Join(ctx.Err(), err)
	}
	if err != nil {
		return verdict, KnowledgeAssessmentState{}, err
	}
	if !attempt.valid() {
		return verdict, KnowledgeAssessmentState{}, errors.New("invalid knowledge assessment attempt")
	}
	if attempt.Status == KnowledgeAssessmentDeferred {
		return verdict, KnowledgeAssessmentState{Status: attempt.Status, Reason: attempt.Reason}, nil
	}
	verdict, err = c.Store.saveKnowledgeAssessment(ctx, owner, updateID, kind, attempt.Verdict)
	return verdict, KnowledgeAssessmentState{}, err
}

func (c KnowledgeCoordinator) RetryAssessment(
	ctx context.Context,
	owner string,
	updateID int64,
	command knowledge.Command,
) (KnowledgeAssessmentState, error) {
	proposals, err := c.Client.KnowledgeProposals(
		ctx,
		owner,
		knowledge.ProposalQuery{Event: command.Event, After: command.ProposalID + 1},
	)
	if err != nil {
		return KnowledgeAssessmentState{}, err
	}
	for _, p := range proposals {
		if p.ID == command.ProposalID && p.Owner == owner && p.Version == command.Version {
			return c.assess(ctx, owner, updateID, p, false)
		}
	}
	return KnowledgeAssessmentState{}, nil
}
