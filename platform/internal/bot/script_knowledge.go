package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptKnowledgePrefix = "knowledge."
const scriptKnowledgeRead = "knowledge.read"
const scriptKnowledgeProposals = "knowledge.proposals"
const scriptKnowledgeReviewQueue = "knowledge.review_queue"
const scriptKnowledgeScopes = "knowledge.scopes"
const scriptKnowledgeMemos = "knowledge.memos"

func (b *Bot) scriptKnowledgeEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return (agenthost.KnowledgeScriptCatalog{Client: b.API, Binding: agenthost.ScriptToolEntry{
		Prepare: b.prepareKnowledgeTool, Execute: b.executeKnowledgeTool, ResultLimit: maxScriptReadBytes,
	}}).Entries(ctx, owner)
}

type scriptKnowledgeArguments struct {
	Event      string `json:"event"`
	Topic      string `json:"topic"`
	FactKey    string `json:"fact_key"`
	Text       string `json:"text"`
	Cursor     string `json:"cursor"`
	ProposalID int64  `json:"proposal_id"`
}

func knowledgeToolProposal(call scriptclient.ToolCall) (agent.KnowledgeProposal, string, error) {
	var args scriptKnowledgeArguments
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return agent.KnowledgeProposal{}, "", err
	}
	p := agent.KnowledgeProposal{
		Name:       strings.TrimPrefix(call.Name, scriptKnowledgePrefix),
		Event:      args.Event,
		Topic:      args.Topic,
		FactKey:    args.FactKey,
		Text:       args.Text,
		ProposalID: args.ProposalID,
	}
	if call.Name == scriptKnowledgeReviewQueue {
		p.Name, p.ReviewQueue = agent.KnowledgeProposals, true
	}
	if call.Name == scriptKnowledgeScopes {
		cursor := args.Cursor
		args.Cursor = ""
		if args != (scriptKnowledgeArguments{}) {
			return p, "", errors.New("invalid knowledge arguments")
		}
		return p, cursor, nil
	}
	if call.Name == scriptKnowledgeMemos {
		if args != (scriptKnowledgeArguments{}) {
			return p, "", errors.New("invalid knowledge arguments")
		}
		return p, "", nil
	}
	if args.Cursor != "" && (p.Name != agent.KnowledgeRead && p.Name != agent.KnowledgeProposals || p.FactKey != "") {
		return p, "", errors.New("invalid knowledge cursor")
	}
	if err := agent.Validate(agent.Plan{View: agent.KnowledgeView, KnowledgeAction: &p}); err != nil {
		return p, "", err
	}
	return p, args.Cursor, nil
}

func (b *Bot) prepareKnowledgeTool(
	ctx context.Context,
	owner string,
	_ int64,
	call scriptclient.ToolCall,
	input agent.Input,
) (agenthost.ScriptToolRecord, error) {
	p, _, err := knowledgeToolProposal(call)
	if err != nil {
		return agenthost.ScriptToolRecord{
			Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted},
		}, err
	}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	return (agenthost.KnowledgeScriptPreparation{
		Domain: b.API, Reader: b.knowledgeReader(), Coordinator: b.knowledgeCoordinator(),
	}).Prepare(ctx, owner, call.Name, p, input, ok && source.owner == owner)
}
