package agenthost

import (
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// ScriptKnowledgeAssessmentResult retains host authority until model projection.
type ScriptKnowledgeAssessmentResult struct {
	knowledge.Result

	Assessment interaction.KnowledgeAssessmentState `json:"assessment"`
}

type ScriptDomainPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
	More       bool   `json:"more"`
}

type ScriptHistoryChunk struct {
	conversation.TextChunk

	NextCursor string `json:"next_cursor"`
}

func ModelMemoryEntries(entries []knowledge.MemoryEntry) []knowledge.MemoryEntry {
	entries = slices.Clone(entries)
	for i := range entries {
		entries[i].ReadAuthorities = nil
	}
	return entries
}

func ModelFacts(items []knowledge.Fact) []knowledge.Fact {
	items = slices.Clone(items)
	for i := range items {
		items[i].ReadAuthorities = nil
	}
	return items
}

func ModelMemos(items []knowledge.Memo) []knowledge.Memo {
	items = slices.Clone(items)
	for i := range items {
		items[i].ReadAuthorities = nil
	}
	return items
}

func ModelProposals(items []knowledge.Proposal) []knowledge.Proposal {
	items = slices.Clone(items)
	for i := range items {
		items[i].ReadAuthorities = nil
	}
	return items
}

func ModelToolEvidence(value any) any {
	switch value := value.(type) {
	case interaction.RegistrationReceiptObservation, *interaction.RegistrationReceiptObservation,
		interaction.RegistrationOperationRead, *interaction.RegistrationOperationRead:
		return modelRegistrationEvidence(value)
	case *knowledge.MemoryPage:
		return ModelToolEvidence(*value)
	case *knowledge.MemoryOverview:
		return ModelToolEvidence(*value)
	case *knowledge.MemoryEntry:
		return ModelToolEvidence(*value)
	case *conversation.Page:
		return ModelToolEvidence(*value)
	case knowledge.MemoryPage:
		value.ReadAuthorities = nil
		value.Entries = ModelMemoryEntries(value.Entries)
		return value
	case knowledge.MemoryOverview:
		value.ReadAuthorities = nil
		value.Summaries = ModelMemoryEntries(value.Summaries)
		return value
	case knowledge.MemoryEntry:
		value.ReadAuthorities = nil
		return value
	case knowledge.Fact:
		value.ReadAuthorities = nil
		return value
	case knowledge.Memo:
		value.ReadAuthorities = nil
		return value
	case knowledge.Proposal:
		value.ReadAuthorities = nil
		return value
	case []knowledge.Memo:
		return ModelMemos(value)
	case ScriptDomainPage[knowledge.Fact]:
		value.Items = ModelFacts(value.Items)
		return value
	case ScriptDomainPage[knowledge.Proposal]:
		value.Items = ModelProposals(value.Items)
		return value
	case knowledge.Result, ScriptKnowledgeAssessmentResult:
		return modelKnowledgeMutationResult(value)
	case conversation.Page:
		return modelConversationPage(value)
	case ScriptHistoryChunk:
		value.ReadAuthorities = nil
		return value
	default:
		return value
	}
}

func modelRegistrationEvidence(value any) any {
	switch value := value.(type) {
	case interaction.RegistrationReceiptObservation:
		value.ReadAuthorities = nil
		return value
	case *interaction.RegistrationReceiptObservation:
		return modelRegistrationEvidence(*value)
	case interaction.RegistrationOperationRead:
		return value.Summaries
	case *interaction.RegistrationOperationRead:
		return value.Summaries
	default:
		return value
	}
}

func modelKnowledgeMutationResult(value any) any {
	switch value := value.(type) {
	case knowledge.Result:
		return ModelKnowledgeResult(value)
	case ScriptKnowledgeAssessmentResult:
		value.Result = ModelKnowledgeResult(value.Result)
		return value
	default:
		return value
	}
}

func RetainModelKnowledgeEvidence(input *agent.Input) error {
	if input.Knowledge == nil {
		return nil
	}
	refs, err := MergeReadAuthorities(input.ReadAuthorities, KnowledgeReadAuthorities(input.Knowledge))
	if err != nil {
		return err
	}
	input.ReadAuthorities = refs
	input.Knowledge.Memos = ModelMemos(input.Knowledge.Memos)
	if input.Knowledge.Memory != nil {
		input.Knowledge.Memory.Overview.ReadAuthorities = nil
		input.Knowledge.Memory.Overview.Summaries = ModelMemoryEntries(input.Knowledge.Memory.Overview.Summaries)
	}
	for i := range input.Knowledge.Reads {
		read := &input.Knowledge.Reads[i]
		read.Facts = ModelFacts(read.Facts)
		read.Proposals = ModelProposals(read.Proposals)
		if read.Memo != nil {
			value := *read.Memo
			value.ReadAuthorities = nil
			read.Memo = &value
		}
	}
	return nil
}

func ModelKnowledgeResult(value knowledge.Result) knowledge.Result {
	value.PrivateDeletion = nil
	value.ReadAuthorities = nil
	if value.Fact != nil {
		item := *value.Fact
		item.ReadAuthorities = nil
		value.Fact = &item
	}
	if value.Memo != nil {
		item := *value.Memo
		item.ReadAuthorities = nil
		value.Memo = &item
	}
	if value.Document != nil {
		item := *value.Document
		item.ReadAuthorities = nil
		value.Document = &item
	}
	if value.Proposal != nil {
		item := *value.Proposal
		item.ReadAuthorities = nil
		value.Proposal = &item
	}
	return value
}

func modelConversationPage(value conversation.Page) conversation.Page {
	value.ReadAuthorities = nil
	value.Events = slices.Clone(value.Events)
	for i := range value.Events {
		value.Events[i].ReadAuthorities = nil
	}
	return value
}
