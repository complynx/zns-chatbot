package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const proposalCursorField = "cursor"
const proposalEventField = "event"
const proposalPaymentAdminField = "payment_admin"

func codexKnowledgeFields(data []byte) error {
	fields, err := codexObject(data)
	if err != nil || len(fields) != 8 {
		return errors.New("invalid knowledge action fields")
	}
	for _, key := range []string{codexNameField, proposalEventField, "topic", "fact_key", codexTextField, "proposal_id", "review_queue", proposalCursorField} {
		if len(fields[key]) == 0 || bytes.Equal(fields[key], []byte("null")) {
			return errors.New("missing knowledge action field")
		}
	}
	var proposal KnowledgeProposal
	if json.Unmarshal(data, &proposal) != nil {
		return errors.New("invalid knowledge action types")
	}
	return validateKnowledgeProposal(proposal)
}

const knowledgeReviewCard = "review_card"
const KnowledgeView = "knowledge"
const KnowledgeRead = "read"
const KnowledgeProposals = "proposals"
const KnowledgeMemoRead = "memo_read"
const MaxKnowledgeReads = 2

// KnowledgeContext contains only same-actor API data. Retrieved facts, suggestions
// and private memos remain untrusted text, even when a curator approved a fact.
type KnowledgeContext struct {
	Memory    *MemoryContext        `json:"memory,omitempty"`
	Scopes    []knowledge.Scope     `json:"scopes"`
	Memos     []knowledge.Memo      `json:"memos"`
	Reads     []KnowledgeReadResult `json:"reads,omitempty"`
	Remaining int                   `json:"remaining"`
	Omitted   bool                  `json:"omitted"`
}

type KnowledgeReadResult struct {
	MemoryState knowledge.MemoryDeletionState `json:"memory_state"`
	More        bool                          `json:"more"`
	NextCursor  string                        `json:"next_cursor,omitempty"`
	Request     KnowledgeProposal             `json:"request"`
	Facts       []knowledge.Fact              `json:"facts,omitempty"`
	Proposals   []knowledge.Proposal          `json:"proposals,omitempty"`
	Memo        *knowledge.Memo               `json:"memo,omitempty"`
	Error       string                        `json:"error,omitempty"`
	Omitted     bool                          `json:"omitted"`
}

// KnowledgeProposal never contains actor, permission, idempotency key or version.
// The host binds those to the current authenticated interaction and API evidence.
type KnowledgeProposal struct {
	Cursor      string `json:"cursor"`
	Name        string `json:"name"`
	Event       string `json:"event"`
	Topic       string `json:"topic"`
	FactKey     string `json:"fact_key"`
	Text        string `json:"text"`
	ProposalID  int64  `json:"proposal_id"`
	ReviewQueue bool   `json:"review_queue"`
}

func IsKnowledgeRead(proposal *KnowledgeProposal) bool {
	return proposal != nil &&
		(proposal.Name == KnowledgeRead || proposal.Name == KnowledgeProposals || proposal.Name == KnowledgeMemoRead)
}

func validateKnowledgeProposal(p KnowledgeProposal) error {
	const maxCursor = 300
	if len(p.Cursor) > maxCursor || (p.Cursor != "" && (p.Name != KnowledgeRead || p.FactKey != "")) {
		return errors.New("invalid knowledge cursor")
	}
	const maxID = 100
	if !boundedKnowledgeText(p.Event, maxID) || !boundedKnowledgeText(p.Topic, maxID) ||
		!boundedKnowledgeText(p.FactKey, maxID) ||
		p.ProposalID < 0 {
		return errors.New("invalid knowledge proposal")
	}
	limit := knowledge.MaxText
	switch p.Name {
	case KnowledgeRead:
		limit = maxID
	case KnowledgeProposals, KnowledgeMemoRead, knowledge.RemoveFact, knowledge.MemoDelete, knowledgeReviewCard:
		limit = 0
	case knowledge.MemoSet:
		limit = knowledge.MaxMemoText
	case knowledge.Suggest, knowledge.Curate:
	default:
		return errors.New("forbidden knowledge proposal")
	}
	if !boundedKnowledgeText(p.Text, limit) {
		return errors.New("knowledge proposal exceeds budget")
	}
	if p.Name != knowledgeReviewCard && p.ProposalID != 0 {
		return errors.New("unexpected proposal target")
	}
	if p.Name != KnowledgeProposals && p.ReviewQueue {
		return errors.New("unexpected review queue")
	}
	if !knowledgeProposalShape(p) {
		return errors.New("invalid knowledge action shape")
	}
	return nil
}

func knowledgeProposalShape(p KnowledgeProposal) bool {
	switch p.Name {
	case KnowledgeRead:
		return p.FactKey == "" || (p.Topic != "" && p.Text == "")
	case KnowledgeProposals:
		return p.Topic == "" && p.FactKey == ""
	case KnowledgeMemoRead, knowledge.MemoSet, knowledge.MemoDelete:
		return p.Event == "" && p.Topic == "" && p.FactKey != ""
	case knowledgeReviewCard:
		return p.ProposalID > 0 && p.Topic == "" && p.FactKey == ""
	default:
		return p.Topic != "" && p.FactKey != ""
	}
}

func boundedKnowledgeText(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && !strings.ContainsRune(value, 0)
}
