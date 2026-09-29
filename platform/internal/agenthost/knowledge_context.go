package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// KnowledgeDomain supplies authenticated knowledge and memory reads.
type KnowledgeDomain interface {
	KnowledgeScopes(context.Context, string) ([]knowledge.Scope, error)
	MemorySummary(context.Context, string, knowledge.MemoryQuery) (knowledge.MemoryOverview, error)
	MemoryDeletions(context.Context, string) (knowledge.MemoryDeletionState, error)
	KnowledgeProposals(context.Context, string, knowledge.ProposalQuery) ([]knowledge.Proposal, error)
	KnowledgeFact(context.Context, string, string, string, string) (knowledge.Fact, error)
	KnowledgePage(context.Context, string, knowledge.Query) (knowledge.FactPage, error)
	Memo(context.Context, string, string) (knowledge.Memo, error)
	Memos(context.Context, string) ([]knowledge.Memo, error)
}

type KnowledgeReadStore interface {
	Knowledge(context.Context, string, int64) ([]agent.KnowledgeReadResult, error)
	ReserveKnowledge(context.Context, string, int64, agent.KnowledgeProposal) (int, error)
	CompleteKnowledge(
		context.Context,
		string,
		int64,
		int,
		agent.KnowledgeReadResult,
	) ([]agent.KnowledgeReadResult, error)
}

// KnowledgeReader owns retained read admission, bounded projection and refresh.
type KnowledgeReader struct {
	Domain KnowledgeDomain
	Store  KnowledgeReadStore
}

const maxKnowledgeContext = 32 * 1024
const maxKnowledgeReadBytes = 12 * 1024

func (k KnowledgeReader) Knowledge(ctx context.Context, owner string, updateID int64) (*agent.KnowledgeContext, error) {
	scopes, err := k.Domain.KnowledgeScopes(ctx, owner)
	if err != nil {
		return nil, err
	}
	overview, err := k.Domain.MemorySummary(ctx, owner, knowledge.MemoryQuery{})
	if err != nil {
		return nil, err
	}
	reads, err := k.Store.Knowledge(ctx, owner, updateID)
	if err != nil {
		return nil, err
	}
	value := &agent.KnowledgeContext{
		Scopes: scopes,
		Memory: &agent.MemoryContext{
			Overview:   overview,
			Navigation: "Use tools.memory.summary/index/search/read/history/sources via script_action; inspect each tool with $help. Empty or incomplete pages do not prove absence.",
		},
		Reads:     reads,
		Remaining: agent.MaxKnowledgeReads - len(reads),
	}
	if err = k.SanitizeKnowledgeReads(ctx, owner, value); err != nil {
		return nil, err
	}
	boundKnowledgeContext(value)
	return value, nil
}

// RefreshKnowledge checks current capabilities; retained reads never preserve grants.
func (k KnowledgeReader) RefreshKnowledge(ctx context.Context, owner string, value *agent.KnowledgeContext) error {
	if value == nil {
		return nil
	}
	scopes, err := k.Domain.KnowledgeScopes(ctx, owner)
	if err != nil {
		return err
	}
	value.Scopes = scopes
	if value.Memory != nil {
		overview, summaryErr := k.Domain.MemorySummary(ctx, owner, knowledge.MemoryQuery{})
		if summaryErr != nil {
			return summaryErr
		}
		value.Memory.Overview = overview
	}
	if err = k.SanitizeKnowledgeReads(ctx, owner, value); err != nil {
		return err
	}
	if err = k.refreshDeletedMemoReads(ctx, owner, value); err != nil {
		return err
	}
	boundKnowledgeContext(value)
	return nil
}

func (k KnowledgeReader) SanitizeKnowledgeReads(
	ctx context.Context,
	owner string,
	value *agent.KnowledgeContext,
) error {
	state, err := k.Domain.MemoryDeletions(ctx, owner)
	if err != nil {
		return err
	}
	RedactKnowledgeReads(value.Reads, state)
	for index := range value.Reads {
		read := &value.Reads[index]
		if !read.Request.ReviewQueue {
			continue
		}
		// Discovery is capped. The event-specific read checks actual current rights;
		// its fresh payload must not replace the existing bounded projection.
		_, err = k.Domain.KnowledgeProposals(
			ctx,
			owner,
			knowledge.ProposalQuery{Event: read.Request.Event, ReviewQueue: true},
		)
		if err == nil {
			continue
		}
		problem, ok := errors.AsType[*core.ProblemError](err)
		if !ok || problem.Status >= http.StatusInternalServerError {
			return err
		}
		*read = agent.KnowledgeReadResult{Request: read.Request, Error: "forbidden", Omitted: read.Omitted}
	}
	return nil
}

// Reserve before I/O. A crash/cancellation consumes its slot rather than giving
// retries a fresh budget. Only this actor/update can see the stored result.

func (k KnowledgeReader) ReadKnowledge(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.KnowledgeProposal,
	input *agent.Input,
) error {
	if input.Knowledge == nil || input.Knowledge.Remaining <= 0 {
		return errors.New("knowledge read budget exhausted")
	}
	index, err := k.Store.ReserveKnowledge(ctx, owner, updateID, p)
	if err != nil {
		return err
	}
	result, err := k.fetchKnowledgeRead(ctx, owner, p)
	if err != nil {
		return err
	}
	boundKnowledgeRead(&result, maxKnowledgeReadBytes)
	reads, err := k.Store.CompleteKnowledge(ctx, owner, updateID, index, result)
	if err != nil {
		return err
	}
	// Keep earlier bounded projections instead of restoring their persisted payloads.
	copy(reads[:index], input.Knowledge.Reads)
	input.Knowledge.Reads = reads
	input.Knowledge.Remaining = agent.MaxKnowledgeReads - len(reads)
	return k.RefreshKnowledge(ctx, owner, input.Knowledge)
}

func (k KnowledgeReader) fetchKnowledgeRead(
	ctx context.Context,
	owner string,
	p agent.KnowledgeProposal,
) (agent.KnowledgeReadResult, error) {
	state, err := k.Domain.MemoryDeletions(ctx, owner)
	result := agent.KnowledgeReadResult{Request: p, MemoryState: state}
	if err != nil {
		return result, err
	}
	switch p.Name {
	case agent.KnowledgeRead:
		if p.FactKey != "" {
			var fact knowledge.Fact
			fact, err = k.Domain.KnowledgeFact(ctx, owner, p.Event, p.Topic, p.FactKey)
			result.Facts = []knowledge.Fact{fact}
		} else {
			var page knowledge.FactPage
			page, err = k.Domain.KnowledgePage(
				ctx,
				owner,
				knowledge.Query{Event: p.Event, Topic: p.Topic, Text: p.Text, Cursor: p.Cursor},
			)
			result.Facts, result.More, result.NextCursor = page.Facts, page.More, page.NextCursor
		}
	case agent.KnowledgeProposals:
		result.Proposals, err = k.Domain.KnowledgeProposals(
			ctx,
			owner,
			knowledge.ProposalQuery{Event: p.Event, ReviewQueue: p.ReviewQueue},
		)
	case agent.KnowledgeMemoRead:
		var memo knowledge.Memo
		memo, err = k.Domain.Memo(ctx, owner, p.FactKey)
		result.Memo = &memo
	default:
		return result, errors.New("invalid knowledge read")
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		problem, ok := errors.AsType[*core.ProblemError](err)
		if !ok || problem.Status >= http.StatusInternalServerError {
			return result, err
		}
		result.Error = problem.Code
	}
	return result, nil
}

func boundKnowledgeRead(read *agent.KnowledgeReadResult, limit int) {
	for {
		data, _ := json.Marshal(read)
		if len(data) <= limit {
			return
		}
		read.Omitted = true
		switch {
		case len(read.Facts) > 0:
			read.Facts = read.Facts[:len(read.Facts)-1]
			read.More = true
			if len(read.Facts) > 0 {
				read.NextCursor = knowledge.CursorAfter(read.Facts[len(read.Facts)-1])
			} else {
				read.NextCursor = read.Request.Cursor
			}
		case len(read.Proposals) > 0:
			read.Proposals = read.Proposals[:len(read.Proposals)-1]
		case read.Memo != nil:
			read.Memo = nil
		default:
			return
		}
	}
}

func boundKnowledgeContext(context *agent.KnowledgeContext) {
	for index := range context.Reads {
		data, _ := json.Marshal(context)
		if len(data) <= maxKnowledgeContext {
			return
		}
		context.Omitted = true
		boundKnowledgeRead(&context.Reads[index], 1)
	}
}

// Refresh only previously observed private memos after a deletion. A stale
// script stays terminal; this snapshot is for the next authorized model call.
func (k KnowledgeReader) refreshDeletedMemoReads(
	ctx context.Context,
	owner string,
	value *agent.KnowledgeContext,
) error {
	deleted := func(read agent.KnowledgeReadResult) bool {
		return read.Error == "memory_deleted" && read.Request.Name == agent.KnowledgeMemoRead
	}
	if !slices.ContainsFunc(value.Reads, deleted) {
		return nil
	}
	fresh := &agent.KnowledgeContext{}
	if _, err := k.Memos(ctx, owner, fresh); err != nil {
		return err
	}
	value.Reads = slices.DeleteFunc(
		value.Reads,
		func(read agent.KnowledgeReadResult) bool { return read.Request.Name == agent.KnowledgeMemoRead },
	)
	value.Reads = append(fresh.Reads, value.Reads...)
	return nil
}
