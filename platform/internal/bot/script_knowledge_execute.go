package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func (b *Bot) executeKnowledgeTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.Memory != nil {
		return b.executeKnowledgeToolCommand(ctx, owner, *record.Memory)
	}
	p, rawCursor, err := knowledgeToolProposal(call)
	if err != nil {
		return nil, err
	}
	if input.Knowledge == nil {
		input.Knowledge = &agent.KnowledgeContext{}
	}
	switch call.Name {
	case scriptKnowledgeScopes:
		return b.readKnowledgeToolScopes(ctx, owner, rawCursor)
	case scriptKnowledgeMemos:
		return b.readKnowledgeToolMemos(ctx, owner, input.Knowledge)
	}
	scope, _ := json.Marshal(p)
	cursor, err := readScriptCursor(rawCursor, owner, call.Name, string(scope))
	if err != nil {
		return nil, err
	}
	state, err := b.API.memoryDeletions(ctx, owner)
	if err != nil {
		return nil, err
	}
	read := agent.KnowledgeReadResult{Request: p, MemoryState: state}
	var result any
	switch p.Name {
	case agent.KnowledgeRead:
		result, read.Facts, err = b.readKnowledgeToolFacts(ctx, owner, p, cursor)
	case agent.KnowledgeProposals:
		result, read.Proposals, err = b.readKnowledgeToolProposals(ctx, owner, p, cursor)
	case agent.KnowledgeMemoRead:
		var memo knowledge.Memo
		memo, err = b.API.Memo(ctx, owner, p.FactKey)
		result, read.Memo = memo, &memo

	default:
		return nil, errors.New("tool unavailable")
	}
	if err != nil {
		return nil, err
	}
	// Only facts actually returned to the worker may bind a later mutation.
	input.Knowledge.Reads = append([]agent.KnowledgeReadResult{read}, input.Knowledge.Reads...)
	return result, nil
}

func (b *Bot) readKnowledgeToolFacts(
	ctx context.Context,
	owner string,
	p agent.KnowledgeProposal,
	cursor scriptReadCursor,
) (any, []knowledge.Fact, error) {
	if p.FactKey != "" {
		fact, err := b.API.KnowledgeFact(ctx, owner, p.Event, p.Topic, p.FactKey)
		return fact, []knowledge.Fact{fact}, err
	}
	remote, err := b.API.KnowledgePage(
		ctx,
		owner,
		knowledge.Query{Event: p.Event, Topic: p.Topic, Text: p.Text, Cursor: cursor.Position},
	)
	if err != nil {
		return nil, nil, err
	}
	next := remote.NextCursor
	if !remote.More {
		next = ""
	}
	page, err := scriptDomainItems(remote.Facts, cursor, next)
	return page, page.Items, err
}

func (b *Bot) readKnowledgeToolProposals(
	ctx context.Context,
	owner string,
	p agent.KnowledgeProposal,
	cursor scriptReadCursor,
) (any, []knowledge.Proposal, error) {
	var after int64
	if cursor.Position != "" {
		parsed, err := strconv.ParseInt(cursor.Position, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, nil, errors.New("invalid proposal cursor")
		}
		after = parsed
	}
	proposals, err := b.API.KnowledgeProposals(
		ctx,
		owner,
		knowledge.ProposalQuery{Event: p.Event, ReviewQueue: p.ReviewQueue, After: after},
	)
	if err != nil {
		return nil, nil, err
	}
	next := ""
	if len(proposals) == knowledge.MaxResults {
		next = strconv.FormatInt(proposals[len(proposals)-1].ID, 10)
	}
	page, err := scriptDomainItems(proposals, cursor, next)
	return page, page.Items, err
}

func (b *Bot) executeKnowledgeToolCommand(ctx context.Context, owner string, command knowledge.Command) (any, error) {
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return nil, errors.New("tool unavailable")
	}
	if command.Name == knowledgeReviewCard {
		return b.showKnowledgeToolReview(ctx, owner, command, source.in.chat)
	}
	result, err := b.API.ExecuteKnowledge(ctx, owner, command)
	if err != nil {
		return nil, err
	}
	if result.Proposal != nil && command.Name == knowledge.Suggest {
		if err = b.assessKnowledgeProposalReceipt(
			ctx,
			owner,
			source.update.ID,
			*result.Proposal,
			"knowledge_assessment:"+strconv.FormatInt(result.Proposal.ID, 10),
		); err != nil {
			return nil, err
		}
	}
	if err = b.saveKnowledgeView(
		ctx,
		owner,
		knowledgeView{Event: command.Event, Mode: knowledgeCommandMode(command.Name)},
	); err != nil {
		return nil, err
	}
	if err = b.RenderKnowledge(ctx, owner, source.in.chat); err != nil {
		return nil, err
	}
	return result, nil
}

func (b *Bot) showKnowledgeToolReview(
	ctx context.Context,
	owner string,
	command knowledge.Command,
	chat int64,
) (any, error) {
	// The fresh authorized queue read checks event rights even after preparation.
	queue, err := b.API.KnowledgeProposals(
		ctx,
		owner,
		knowledge.ProposalQuery{Event: command.Event, ReviewQueue: true, After: command.ProposalID + 1},
	)
	if err != nil {
		return nil, err
	}
	found := false
	for _, proposal := range queue {
		found = found || proposal.ID == command.ProposalID
	}
	if !found {
		return nil, errors.New("proposal changed; refresh review queue")
	}
	if err = b.saveKnowledgeView(
		ctx,
		owner,
		knowledgeView{Event: command.Event, Mode: knowledgeReviewMode, After: command.ProposalID + 1},
	); err != nil {
		return nil, err
	}
	if err = b.RenderKnowledge(ctx, owner, chat); err != nil {
		return nil, err
	}
	return map[string]bool{"manual_review_required": true}, nil
}

func (b *Bot) readKnowledgeToolScopes(ctx context.Context, owner, rawCursor string) (any, error) {
	cursor, err := readScriptCursor(rawCursor, owner, scriptKnowledgeScopes, "")
	if err != nil {
		return nil, err
	}
	remote, err := b.API.knowledgeScopePage(ctx, owner, cursor.Position)
	if err != nil {
		return nil, err
	}
	return scriptDomainItems(remote.Items, cursor, remote.NextCursor)
}

// List reads carry the same deletion generation as exact memo reads. Keeping a
// second unversioned copy in KnowledgeContext.Memos would bypass sanitization.
func (b *Bot) readKnowledgeToolMemos(
	ctx context.Context,
	owner string,
	input *agent.KnowledgeContext,
) ([]knowledge.Memo, error) {
	state, err := b.API.memoryDeletions(ctx, owner)
	if err != nil {
		return nil, err
	}
	memos, err := b.API.Memos(ctx, owner)
	if err != nil {
		return nil, err
	}
	reads := make([]agent.KnowledgeReadResult, 0, len(memos))
	for index := range memos {
		reads = append(reads, agent.KnowledgeReadResult{
			Request:     agent.KnowledgeProposal{Name: agent.KnowledgeMemoRead, FactKey: memos[index].Key},
			MemoryState: state, Memo: &memos[index],
		})
	}
	input.Reads = append(reads, input.Reads...)
	return memos, nil
}
