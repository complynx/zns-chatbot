package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const knowledgePendingFilter = "pending_filter"
const knowledgeReviewCard = "review_card"

func (b *Bot) bindKnowledgeCommand(
	ctx context.Context,
	owner string,
	p *agent.KnowledgeProposal,
	input *agent.KnowledgeContext,
) (*knowledge.Command, error) {
	if p.Name != knowledge.MemoSet || input == nil {
		return proposedKnowledgeCommand(p, input)
	}
	if _, observed := observedMemoVersion(input, p.FactKey); observed {
		return proposedKnowledgeCommand(p, input)
	}
	memo, err := b.API.Memo(ctx, owner, p.FactKey)
	if err != nil {
		return nil, err
	}
	if memo.Active {
		return nil, errors.New("memo changed since current context")
	}
	// An inactive key cannot overwrite an unseen active preference. Its version
	// still protects against activation before execution.
	input.Reads = append(input.Reads, agent.KnowledgeReadResult{Memo: &memo})
	return proposedKnowledgeCommand(p, input)
}

// Bind versions to evidence the model actually received. Replays retain these
// versions; the domain checks current authorization before its idempotency cache.
func proposedKnowledgeCommand(p *agent.KnowledgeProposal, input *agent.KnowledgeContext) (*knowledge.Command, error) {
	if input == nil {
		return nil, errors.New("knowledge context missing")
	}
	c := &knowledge.Command{Name: p.Name, Event: p.Event, Topic: p.Topic, FactKey: p.FactKey, Text: p.Text}
	switch p.Name {
	case knowledge.Suggest:
		return c, nil
	case knowledge.Curate, knowledge.RemoveFact:
		if !knowledgeCapability(input.Scopes, p.Event, true) {
			return nil, errors.New("knowledge permission missing")
		}
		if version, ok := observedFactVersion(input.Reads, *p); ok {
			c.Version = version
			return c, nil
		}
	case knowledge.MemoSet, knowledge.MemoDelete:
		if version, ok := observedMemoVersion(input, p.FactKey); ok {
			c.Version = version
			return c, nil
		}
	case knowledgeReviewCard:
		if !knowledgeCapability(input.Scopes, p.Event, false) {
			return nil, errors.New("knowledge permission missing")
		}
		if observedReview(input.Reads, *p) {
			c.ProposalID = p.ProposalID
			return c, nil
		}
	}
	return nil, errors.New("knowledge target requires a current read")
}

func observedFactVersion(reads []agent.KnowledgeReadResult, p agent.KnowledgeProposal) (int64, bool) {
	for _, read := range reads {
		if read.Error != "" {
			continue
		}
		for _, fact := range read.Facts {
			if fact.Event == p.Event && fact.Topic == p.Topic && fact.Key == p.FactKey && !fact.HistoricalFallback {
				return fact.Version, true
			}
		}
	}
	return 0, false
}

func observedMemoVersion(input *agent.KnowledgeContext, key string) (int64, bool) {
	for _, memo := range input.Memos {
		if memo.Key == key {
			return memo.Version, true
		}
	}
	for _, read := range input.Reads {
		if read.Error == "" && read.Memo != nil && read.Memo.Key == key {
			return read.Memo.Version, true
		}
	}
	return 0, false
}

func observedReview(reads []agent.KnowledgeReadResult, p agent.KnowledgeProposal) bool {
	for _, read := range reads {
		if read.Error != "" || !read.Request.ReviewQueue {
			continue
		}
		for _, proposal := range read.Proposals {
			if proposal.ID == p.ProposalID && proposal.Event == p.Event {
				return true
			}
		}
	}
	return false
}

func (b *Bot) assessKnowledgeProposal(ctx context.Context, owner string, id int64, p knowledge.Proposal) error {
	return b.assessKnowledgeProposalReceipt(ctx, owner, id, p, "knowledge_assessment")
}

func (b *Bot) assessKnowledgeProposalReceipt(
	ctx context.Context,
	owner string,
	id int64,
	p knowledge.Proposal,
	receipt string,
) error {
	if p.State != knowledgePendingFilter {
		return nil
	}
	classifier, ok := b.Model.(agent.KnowledgeClassifier)
	if !ok {
		return nil
	}
	var verdict agent.KnowledgeAssessment
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, receipt).
		Scan(&verdict)
	if errors.Is(err, pgx.ErrNoRows) {
		verdict, err = classifier.AssessKnowledge(
			ctx,
			agent.KnowledgeAssessmentInput{Event: p.Event, Topic: p.Topic, FactKey: p.FactKey, Text: p.Text},
		)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		if err = b.record(ctx, owner, id, receipt, verdict); err != nil {
			return err
		}
		err = b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, id, receipt).
			Scan(&verdict)
	}
	if err != nil {
		return err
	}
	_, err = b.API.AssessMemoryProposal(
		ctx,
		owner,
		knowledge.Assessment{
			Key:        "tg-assess-" + strconv.FormatInt(p.ID, 10),
			ProposalID: p.ID,
			Version:    p.Version,
			Worthwhile: verdict.Worthwhile,
			Reason:     verdict.Reason,
		},
	)
	return err
}

func (b *Bot) retryKnowledgeAssessment(ctx context.Context, owner string, id int64, c knowledge.Command) error {
	proposals, err := b.API.KnowledgeProposals(
		ctx,
		owner,
		knowledge.ProposalQuery{Event: c.Event, After: c.ProposalID + 1},
	)
	if err != nil {
		return err
	}
	for _, p := range proposals {
		if p.ID == c.ProposalID && p.Owner == owner && p.Version == c.Version {
			if err = b.assessKnowledgeProposal(ctx, owner, id, p); err != nil {
				return err
			}
		}
	}
	return b.saveKnowledgeView(ctx, owner, knowledgeView{Event: c.Event, Mode: knowledgeOwnMode})
}
