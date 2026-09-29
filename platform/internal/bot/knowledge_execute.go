package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (b *Bot) executeKnowledgeCommand(ctx context.Context, in incoming, id int64, c knowledge.Command) (string, error) {
	return b.executeKnowledgeWithSource(ctx, in, id, c, nil)
}

func (b *Bot) executePlannedKnowledge(
	ctx context.Context,
	in incoming,
	id int64,
	plan interaction.SavedPlan,
) (string, error) {
	var source *readsource.Derivation
	if plan.PassAuthority != nil {
		value := readsource.Derivation{
			PrivateHistory: plan.PassAuthority.PrivateHistory,
			Generation:     &plan.HistoryGeneration,
			Authorities:    plan.PassAuthority.ReadAuthorities,
		}.Clone()
		source = &value
	}
	return b.executeKnowledgeWithSource(ctx, in, id, *plan.KnowledgeCommand, source)
}

func (b *Bot) executeKnowledgeWithSource(
	ctx context.Context,
	in incoming,
	id int64,
	c knowledge.Command,
	source *readsource.Derivation,
) (string, error) {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", err
	}
	outcome, err := b.knowledgeCoordinator().Execute(ctx, in.owner, id, c, source)
	if err != nil {
		return "", err
	}
	if outcome.Kind == interaction.KnowledgeReviewTargetKind {
		if err = b.saveKnowledgeView(
			ctx,
			in.owner,
			knowledgeView{Event: c.Event, Mode: knowledgeReviewMode},
		); err != nil {
			return "", err
		}
		return i18n.Translate(preference.Language, i18n.KnowledgeReview, nil)
	}
	notice := i18n.KnowledgeSaved
	switch {
	case outcome.Refusal != nil:
		notice = i18n.KnowledgeUnavailable
	case outcome.Assessment.Status == interaction.KnowledgeAssessmentDeferred:
		notice = i18n.KnowledgePendingFilter
	case outcome.Kind == interaction.KnowledgeSuggested:
		notice = i18n.KnowledgeSuggested
	}
	mode := knowledgeCommandMode(c.Name)
	if err = b.saveKnowledgeView(ctx, in.owner, knowledgeView{Event: c.Event, Mode: mode}); err != nil {
		return "", err
	}
	return i18n.Translate(preference.Language, notice, nil)
}

func knowledgeCommandMode(name string) string {
	switch name {
	case knowledge.MemoSet, knowledge.MemoDelete:
		return knowledgeMemoMode
	case knowledge.Review:
		return knowledgeReviewMode
	case knowledge.Curate, knowledge.RemoveFact:
		return knowledgeFactsMode
	default:
		return knowledgeOwnMode
	}
}
