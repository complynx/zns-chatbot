package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func (b *Bot) executeKnowledgeTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.Memory != nil {
		return b.executeKnowledgeToolCommand(ctx, owner, *record.Memory, record.Source)
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
		return b.knowledgeReader().Memos(ctx, owner, input.Knowledge)
	}
	scope, _ := json.Marshal(p)
	cursor, err := readScriptCursor(rawCursor, owner, call.Name, string(scope))
	if err != nil {
		return nil, err
	}
	state, err := b.API.MemoryDeletions(ctx, owner)
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

func (b *Bot) executeKnowledgeToolCommand(
	ctx context.Context,
	owner string,
	command knowledge.Command,
	derivation *readsource.Derivation,
) (any, error) {
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return nil, errors.New("tool unavailable")
	}
	if command.Name == knowledgeReviewCard {
		return b.showKnowledgeToolReview(ctx, owner, command, source.in.chat)
	}
	outcome, err := b.knowledgeCoordinator().ExecuteScript(ctx, owner, source.update.ID, command, derivation)
	if err != nil {
		return nil, err
	}
	if outcome.Refusal != nil {
		return nil, outcome.Refusal
	}
	if command.Name != knowledge.MemoSet && command.Name != knowledge.MemoDelete {
		if err = b.refreshScriptKnowledge(ctx, owner, command); err != nil {
			return nil, err
		}
	}
	return knowledgeScriptResult(outcome), nil
}

// knowledgeScriptResult retains host evidence until the script encoder captures it.
func knowledgeScriptResult(outcome interaction.KnowledgeOutcome) any {
	if outcome.Assessment.Status == interaction.KnowledgeAssessmentDeferred {
		return agenthost.ScriptKnowledgeAssessmentResult{Result: outcome.Result, Assessment: outcome.Assessment}
	}
	return outcome.Result
}

func (b *Bot) showKnowledgeToolReview(
	ctx context.Context,
	owner string,
	command knowledge.Command,
	chat int64,
) (any, error) {
	target, err := b.knowledgeCoordinator().ReviewTarget(ctx, owner, command)
	if err != nil {
		return nil, err
	}
	if err = b.saveKnowledgeView(
		ctx,
		owner,
		knowledgeView{Event: command.Event, Mode: knowledgeReviewMode, After: target.ProposalID + 1},
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
	remote, err := b.API.KnowledgeScopePage(ctx, owner, cursor.Position)
	if err != nil {
		return nil, err
	}
	return scriptDomainItems(remote.Items, cursor, remote.NextCursor)
}
