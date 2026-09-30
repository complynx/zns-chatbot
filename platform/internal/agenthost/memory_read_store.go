package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// MemoryReadState keeps domain deletion fences and retained host state in one application transaction.
type MemoryReadState interface {
	ReserveKnowledge(context.Context, string, int64, agent.KnowledgeProposal) (int, error)
	ReconcileMemory(context.Context, string, knowledge.MemoryDeletionState) error
	CompleteKnowledge(
		context.Context,
		string,
		int64,
		int,
		agent.KnowledgeReadResult,
	) ([]agent.KnowledgeReadResult, error)
}

// MemoryReadStore uses the application pool; the transport pool has no private-memory capability.
type MemoryReadStore struct{ DB *pgxpool.Pool }

const maxMemoryReadBytes = 1 << 20

func validateMemoryReadArray(raw []byte) error {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' {
		return errors.New("retained memory state must be a JSON array")
	}
	return nil
}

func memoryReadStateTooLarge() error {
	return &core.ProblemError{Status: http.StatusRequestEntityTooLarge, Code: "memory_read_state_too_large"}
}

// Leave room for the HTTP encoder's terminating newline on the same local contract.
func marshalMemoryReads(reads []agent.KnowledgeReadResult) ([]byte, error) {
	raw, err := json.Marshal(reads)
	if err != nil {
		return nil, err
	}
	if len(raw) >= maxMemoryReadBytes {
		return nil, memoryReadStateTooLarge()
	}
	return raw, nil
}

func validMemoryReadInput(owner string, updateID int64, value any) error {
	if owner == "" || updateID <= 0 {
		return &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_memory_read_state"}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > maxMemoryReadBytes {
		return memoryReadStateTooLarge()
	}
	return nil
}
func (s MemoryReadStore) ReserveKnowledge(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.KnowledgeProposal,
) (int, error) {
	if err := validMemoryReadInput(owner, updateID, p); err != nil {
		return 0, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := knowledge.LockMemoryDeletions(ctx, tx, owner)
	if err != nil {
		return 0, err
	}
	if err = knowledgeReadLock(ctx, tx, owner, updateID); err != nil {
		return 0, core.DatabaseOperationContextError(ctx, err)
	}
	var reads []agent.KnowledgeReadResult
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, knowledgeReadsKind).
		Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, core.DatabaseOperationContextError(ctx, err)
	}
	if err == nil {
		if err = validateMemoryReadArray(raw); err != nil {
			return 0, err
		}
		if err = json.Unmarshal(raw, &reads); err != nil {
			return 0, err
		}
	}
	RedactKnowledgeReads(reads, state)
	if len(reads) >= agent.MaxKnowledgeReads {
		return 0, errors.New("knowledge read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, agent.KnowledgeReadResult{Request: p, MemoryState: state, Error: "interrupted"})
	raw, err = marshalMemoryReads(reads)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, knowledgeReadsKind, raw)
	if err != nil {
		return 0, core.DatabaseOperationContextError(ctx, err)
	}
	return index, core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}

func (s MemoryReadStore) ReconcileMemory(ctx context.Context, owner string, _ knowledge.MemoryDeletionState) error {
	if err := validMemoryReadInput(owner, 1, nil); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A caller's captured epochs can be older than an already committed read.
	// Acquire current domain generations before taking interaction row locks.
	state, err := knowledge.LockMemoryDeletions(ctx, tx, owner)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT update_id,kind,content FROM bot.interactions WHERE owner=$1 AND kind IN ($2,$4)
 AND (jsonb_typeof(content)<>'array' OR EXISTS(
 SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(content)='array' THEN content ELSE '[]'::jsonb END) r
 WHERE COALESCE(r->'memory_state','{}'::jsonb)<>$3::jsonb))
 ORDER BY update_id,kind LIMIT 100 FOR UPDATE SKIP LOCKED`, owner, scriptRunsKind, state, knowledgeReadsKind)
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	type retained struct {
		updateID int64
		kind     string
		content  json.RawMessage
	}
	var batch []retained
	for rows.Next() {
		var item retained
		if err = rows.Scan(&item.updateID, &item.kind, &item.content); err != nil {
			rows.Close()
			return core.DatabaseOperationContextError(ctx, err)
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return core.DatabaseOperationContextError(ctx, err)
	}
	for _, item := range batch {
		if err = validateMemoryReadArray(item.content); err != nil {
			return err
		}
		content, redactErr := RedactMemoryInteraction(item.kind, item.content, state)
		if redactErr != nil {
			return redactErr
		}
		if _, err = tx.Exec(
			ctx,
			`UPDATE bot.interactions SET content=$3 WHERE owner=$1 AND update_id=$2 AND kind=$4`,
			owner,
			item.updateID,
			content,
			item.kind,
		); err != nil {
			return core.DatabaseOperationContextError(ctx, err)
		}
	}
	return core.DatabaseOperationContextError(ctx, tx.Commit(ctx))
}

func (s MemoryReadStore) CompleteKnowledge(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	result agent.KnowledgeReadResult,
) ([]agent.KnowledgeReadResult, error) {
	if err := validMemoryReadInput(owner, updateID, result); err != nil {
		return nil, err
	}
	if index < 0 || index >= agent.MaxKnowledgeReads {
		return nil, errors.New("knowledge read reservation missing")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := knowledge.LockMemoryDeletions(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if err = knowledgeReadLock(ctx, tx, owner, updateID); err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	var reads []agent.KnowledgeReadResult
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, knowledgeReadsKind).
		Scan(&raw); err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	if err = validateMemoryReadArray(raw); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &reads); err != nil {
		return nil, err
	}
	if index < 0 || index >= len(reads) {
		return nil, errors.New("knowledge read reservation missing")
	}
	// Retirement belongs to the reservation, not the delayed fetch's epoch.
	RedactKnowledgeReads(reads, state)
	if !reads[index].Omitted {
		result.Request = reads[index].Request
		reads[index] = result
	}
	// Reconcile the locked current row, including siblings, against the
	// domain generations retained through commit. A late fetch cannot revive it.
	RedactKnowledgeReads(reads, state)
	raw, err = marshalMemoryReads(reads)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		updateID,
		knowledgeReadsKind,
		raw,
	)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	return reads, nil
}
