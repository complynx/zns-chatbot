package agenthost

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

type KnowledgePreparationDomain interface {
	MemoryDeletions(context.Context, string) (knowledge.MemoryDeletionState, error)
	KnowledgeScope(context.Context, string, string) (knowledge.Scope, error)
}

// KnowledgeScriptPreparation owns admission of parsed knowledge calls. The
// transport supplies sourceBound from its trusted caller context, never from
// tool arguments. The shared coordinator binds only observed current versions.
type KnowledgeScriptPreparation struct {
	Domain      KnowledgePreparationDomain
	Reader      KnowledgeReader
	Coordinator interaction.KnowledgeCoordinator
}

func (c KnowledgeScriptPreparation) Prepare(ctx context.Context, owner, name string,
	p agent.KnowledgeProposal, input agent.Input, sourceBound bool,
) (ScriptToolRecord, error) {
	record := ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: name, Error: scriptInterrupted}}
	if p.ReviewQueue {
		record.KnowledgeRead = &knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: p.Event}
	}
	if agent.IsKnowledgeRead(&p) || name == scriptKnowledgeScopes || name == scriptKnowledgeMemos {
		state, err := c.Domain.MemoryDeletions(ctx, owner)
		if err != nil {
			return record, err
		}
		record.MemoryReadState = &state
		return record, nil
	}
	if !sourceBound {
		return record, errors.New("tool unavailable")
	}
	if input.Knowledge == nil {
		input.Knowledge = &agent.KnowledgeContext{}
	}
	scope, err := c.Domain.KnowledgeScope(ctx, owner, p.Event)
	if err != nil {
		return record, err
	}
	input.Knowledge.Scopes = []knowledge.Scope{scope}
	if err = c.Reader.SanitizeKnowledgeReads(ctx, owner, input.Knowledge); err != nil {
		return record, err
	}
	record.Memory, err = c.Coordinator.Bind(ctx, owner, &p, input.Knowledge)
	return record, err
}
