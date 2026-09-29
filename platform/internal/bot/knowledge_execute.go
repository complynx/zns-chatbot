package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (b *Bot) executeKnowledgeCommand(ctx context.Context, in incoming, id int64, c knowledge.Command) (string, error) {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", err
	}
	if c.Name == knowledgeReviewCard {
		if err = b.saveKnowledgeView(
			ctx,
			in.owner,
			knowledgeView{Event: c.Event, Mode: knowledgeReviewMode},
		); err != nil {
			return "", err
		}
		return i18n.Translate(preference.Language, i18n.KnowledgeReview, nil)
	}
	if c.Key == "" {
		c.Key = "tg-knowledge-" + strconv.FormatInt(id, 10)
	}
	result, err := b.API.ExecuteKnowledge(ctx, in.owner, c)
	if err == nil && c.Name != knowledge.Review {
		// The privacy-aware request archive already exists before executePlan.
		// Complete provenance from the trusted update, including exact retries.
		if sourceErr := b.API.AttachMemorySources(ctx, in.owner, c.Key, id); sourceErr != nil {
			return "", sourceErr
		}
	}
	notice := i18n.KnowledgeSaved
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		problem, ok := errors.AsType[*core.ProblemError](err)
		if !ok || problem.Status >= http.StatusInternalServerError {
			return "", err
		}
		notice = i18n.KnowledgeUnavailable
	} else if result.Proposal != nil && c.Name == knowledge.Suggest {
		if err = b.assessKnowledgeProposal(ctx, in.owner, id, *result.Proposal); err != nil {
			return "", err
		}
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
