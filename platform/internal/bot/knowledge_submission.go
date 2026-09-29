package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func knowledgeProposalSubmission(owner string, p knowledge.Proposal, review bool) *knowledge.Submission {
	if review || p.Owner != owner || p.State != knowledge.AwaitingSubmission {
		return nil
	}
	return &knowledge.Submission{
		ProposalID: p.ID, Version: p.Version, Event: p.Event,
		Topic: p.Topic, FactKey: p.FactKey, Text: p.Text,
	}
}

// Only an owner-scoped stored button reaches this host operation. Agent plans
// cannot submit consent through the generic knowledge command surface.
func (b *Bot) submitKnowledgeProposal(ctx context.Context, in incoming, input knowledge.Submission) (string, error) {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", err
	}
	outcome, err := b.knowledgeCoordinator().Submit(ctx, in.owner, input)
	if err != nil {
		return "", err
	}
	notice := i18n.KnowledgeSaved
	if outcome.Refusal != nil {
		notice = i18n.KnowledgeUnavailable
	}
	if err = b.saveKnowledgeView(ctx, in.owner, knowledgeView{Event: input.Event, Mode: knowledgeOwnMode}); err != nil {
		return "", err
	}
	return i18n.Translate(preference.Language, notice, nil)
}
