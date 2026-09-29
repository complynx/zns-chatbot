package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const knowledgeReadsKind = "knowledge_reads"
const maxKnowledgeContext = 32 * 1024
const maxKnowledgeReadBytes = 12 * 1024

func (b *Bot) reauthorizeModelContext(ctx context.Context, owner string, input *agent.Input) error {
	if err := b.addFoodHint(ctx, owner, input); err != nil {
		return err
	}
	if input.Script != nil && input.Script.UpdateID != 0 {
		if err := b.addScriptContext(ctx, owner, input.Script.UpdateID, input); err != nil {
			return err
		}
	}
	if err := b.reauthorizeBusinessContext(ctx, owner, input); err != nil {
		return err
	}
	if err := b.reauthorizeKnowledgeContext(ctx, owner, input.Knowledge); err != nil {
		return err
	}
	if err := b.reauthorizeRegistrationContext(ctx, owner, input.Registration); err != nil {
		return err
	}
	return b.reauthorizeHistoryContext(ctx, owner, input)
}

func (b *Bot) addSupportingContext(ctx context.Context, in incoming, updateID int64, input *agent.Input) error {
	if err := b.addFoodHint(ctx, in.owner, input); err != nil {
		return err
	}
	b.addLineupContext(input)
	if err := b.addAssetContext(ctx, in, input); err != nil {
		return err
	}
	if err := b.addKnowledgeContext(ctx, in.owner, updateID, input); err != nil {
		return err
	}
	if err := b.addScriptContext(ctx, in.owner, updateID, input); err != nil {
		return err
	}
	if err := b.addRegistrationContext(ctx, in, updateID, input); err != nil {
		return err
	}
	return b.addHistoryContext(ctx, in.owner, updateID, input)
}

func (b *Bot) addKnowledgeContext(ctx context.Context, owner string, updateID int64, input *agent.Input) error {
	scopes, err := b.API.KnowledgeScopes(ctx, owner)
	if err != nil {
		return err
	}
	overview, err := b.API.MemorySummary(ctx, owner, knowledge.MemoryQuery{})
	if err != nil {
		return err
	}
	reads, err := b.knowledgeReads(ctx, owner, updateID)
	if err != nil {
		return err
	}
	input.Knowledge = &agent.KnowledgeContext{
		Scopes: scopes,
		Memory: &agent.MemoryContext{
			Overview:   overview,
			Navigation: "Use tools.memory.summary/index/search/read/history/sources via script_action; inspect each tool with $help. Empty or incomplete pages do not prove absence.",
		},
		Reads:     reads,
		Remaining: agent.MaxKnowledgeReads - len(reads),
	}
	if err = b.sanitizeKnowledgeReads(ctx, owner, input.Knowledge); err != nil {
		return err
	}
	boundKnowledgeContext(input.Knowledge)
	return nil
}

// Refresh capabilities before model invocation; persisted reads never preserve grants.
func (b *Bot) reauthorizeKnowledgeContext(ctx context.Context, owner string, value *agent.KnowledgeContext) error {
	if value == nil {
		return nil
	}
	scopes, err := b.API.KnowledgeScopes(ctx, owner)
	if err != nil {
		return err
	}
	value.Scopes = scopes
	if value.Memory != nil {
		overview, summaryErr := b.API.MemorySummary(ctx, owner, knowledge.MemoryQuery{})
		if summaryErr != nil {
			return summaryErr
		}
		value.Memory.Overview = overview
	}
	if err = b.sanitizeKnowledgeReads(ctx, owner, value); err != nil {
		return err
	}
	boundKnowledgeContext(value)
	return nil
}

func (b *Bot) sanitizeKnowledgeReads(ctx context.Context, owner string, value *agent.KnowledgeContext) error {
	state, err := b.API.memoryDeletions(ctx, owner)
	if err != nil {
		return err
	}
	redactKnowledgeReads(value.Reads, state)
	for index := range value.Reads {
		read := &value.Reads[index]
		if !read.Request.ReviewQueue {
			continue
		}
		// Discovery is capped. The event-specific read checks actual current rights;
		// its fresh payload must not replace the existing bounded projection.
		_, err = b.API.KnowledgeProposals(
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
		*read = agent.KnowledgeReadResult{Request: read.Request, Error: mediaForbidden, Omitted: read.Omitted}
	}
	return nil
}

func knowledgeCapability(scopes []knowledge.Scope, event string, curate bool) bool {
	for _, scope := range scopes {
		if scope.Event == event {
			if curate {
				return scope.CanCurate
			}
			return scope.CanReview
		}
	}
	return false
}

func (b *Bot) knowledgeReads(ctx context.Context, owner string, updateID int64) ([]agent.KnowledgeReadResult, error) {
	var reads []agent.KnowledgeReadResult
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, knowledgeReadsKind).
		Scan(&reads)
	if errors.Is(err, pgx.ErrNoRows) {
		return []agent.KnowledgeReadResult{}, nil
	}
	return reads, err
}

// Reserve before I/O. A crash/cancellation consumes its slot rather than giving
// retries a fresh budget. Only this actor/update can see the stored result.
func (b *Bot) reserveKnowledgeRead(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.KnowledgeProposal,
) (int, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = knowledgeReadLock(ctx, tx, owner, updateID); err != nil {
		return 0, err
	}
	var reads []agent.KnowledgeReadResult
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, knowledgeReadsKind).
		Scan(&reads)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if len(reads) >= agent.MaxKnowledgeReads {
		return 0, errors.New("knowledge read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, agent.KnowledgeReadResult{Request: p, Error: "interrupted"})
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, knowledgeReadsKind, reads)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func knowledgeReadLock(ctx context.Context, tx pgx.Tx, owner string, updateID int64) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"knowledge-read:"+owner+":"+strconv.FormatInt(updateID, 10),
	)
	return err
}

func (b *Bot) performKnowledgeRead(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.KnowledgeProposal,
	input *agent.Input,
) error {
	if input.Knowledge == nil || input.Knowledge.Remaining <= 0 {
		return errors.New("knowledge read budget exhausted")
	}
	index, err := b.reserveKnowledgeRead(ctx, owner, updateID, p)
	if err != nil {
		return err
	}
	result, err := b.fetchKnowledgeRead(ctx, owner, p)
	if err != nil {
		return err
	}
	boundKnowledgeRead(&result, maxKnowledgeReadBytes)
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = knowledgeReadLock(ctx, tx, owner, updateID); err != nil {
		return err
	}
	var reads []agent.KnowledgeReadResult
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, knowledgeReadsKind).
		Scan(&reads); err != nil {
		return err
	}
	if index >= len(reads) {
		return errors.New("knowledge read reservation missing")
	}
	reads[index] = result
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		updateID,
		knowledgeReadsKind,
		reads,
	)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Keep earlier bounded projections instead of restoring their persisted payloads.
	copy(reads[:index], input.Knowledge.Reads)
	input.Knowledge.Reads = reads
	input.Knowledge.Remaining = agent.MaxKnowledgeReads - len(reads)
	return b.reauthorizeKnowledgeContext(ctx, owner, input.Knowledge)
}

func (b *Bot) fetchKnowledgeRead(
	ctx context.Context,
	owner string,
	p agent.KnowledgeProposal,
) (agent.KnowledgeReadResult, error) {
	state, err := b.API.memoryDeletions(ctx, owner)
	result := agent.KnowledgeReadResult{Request: p, MemoryState: state}
	if err != nil {
		return result, err
	}
	switch p.Name {
	case agent.KnowledgeRead:
		if p.FactKey != "" {
			var fact knowledge.Fact
			fact, err = b.API.KnowledgeFact(ctx, owner, p.Event, p.Topic, p.FactKey)
			result.Facts = []knowledge.Fact{fact}
		} else {
			var page knowledge.FactPage
			page, err = b.API.KnowledgePage(
				ctx,
				owner,
				knowledge.Query{Event: p.Event, Topic: p.Topic, Text: p.Text, Cursor: p.Cursor},
			)
			result.Facts, result.More, result.NextCursor = page.Facts, page.More, page.NextCursor
		}
	case agent.KnowledgeProposals:
		result.Proposals, err = b.API.KnowledgeProposals(
			ctx,
			owner,
			knowledge.ProposalQuery{Event: p.Event, ReviewQueue: p.ReviewQueue},
		)
	case agent.KnowledgeMemoRead:
		var memo knowledge.Memo
		memo, err = b.API.Memo(ctx, owner, p.FactKey)
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
