package bot

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (c APIClient) memoryDeletions(ctx context.Context, owner string) (knowledge.MemoryDeletionState, error) {
	var result knowledge.MemoryDeletionState
	err := c.memoryRead(ctx, owner, "deletions", url.Values{}, &result)
	return result, err
}

func redactKnowledgeReads(reads []agent.KnowledgeReadResult, state knowledge.MemoryDeletionState) {
	for index := range reads {
		if reads[index].MemoryState == state {
			continue
		}
		request := reads[index].Request
		request.Text = ""
		reads[index] = agent.KnowledgeReadResult{
			Request:     request,
			MemoryState: state,
			Error:       "memory_deleted",
			Omitted:     true,
		}
	}
}

// Arbitrary script output can transform remembered text, so deletion invalidates
// the complete result rather than attempting unreliable substring redaction.
func redactDeletedScript(record *scriptRecord, state knowledge.MemoryDeletionState) bool {
	if record.MemoryState == state && !record.MemoryRedacted {
		return false
	}
	record.MemoryState = state
	record.MemoryRedacted = true
	record.Request.Code, record.Request.InputJSON, record.Run.Code = "", "", ""
	record.Run.Result = json.RawMessage(`{"omitted":true,"reason":"memory_deleted"}`)
	record.Run.Calls = nil
	for index := range record.Calls {
		call := &record.Calls[index]
		if call.Memory != nil {
			call.Memory.Text = ""
		}
		if strings.HasPrefix(call.Outcome.Name, "memory.") ||
			strings.HasPrefix(call.Outcome.Name, scriptKnowledgePrefix) {
			call.Outcome.Result = json.RawMessage(`{"omitted":true,"reason":"memory_deleted"}`)
		}
	}
	return true
}

// Reconcile the actor's older script ledgers in bounded batches. The current
// update is independently reconciled before use, even if older rows remain.
func (b *Bot) reconcileMemoryScripts(ctx context.Context, owner string, state knowledge.MemoryDeletionState) error {
	if state == (knowledge.MemoryDeletionState{}) {
		return nil
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT update_id,kind,content FROM bot.interactions WHERE owner=$1 AND kind IN ($2,$4)
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(content) r WHERE COALESCE(r->'memory_state','{}'::jsonb)<>$3::jsonb)
 ORDER BY update_id,kind LIMIT 100 FOR UPDATE SKIP LOCKED`, owner, scriptRunsKind, state, knowledgeReadsKind)
	if err != nil {
		return err
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
			return err
		}
		batch = append(batch, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range batch {
		content, redactErr := redactMemoryInteraction(item.kind, item.content, state)
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
			return err
		}
	}
	return tx.Commit(ctx)
}

func redactMemoryInteraction(
	kind string,
	content json.RawMessage,
	state knowledge.MemoryDeletionState,
) (json.RawMessage, error) {
	if kind == knowledgeReadsKind {
		var reads []agent.KnowledgeReadResult
		if err := json.Unmarshal(content, &reads); err != nil {
			return nil, err
		}
		redactKnowledgeReads(reads, state)
		return json.Marshal(reads)
	}
	var records []scriptRecord
	if err := json.Unmarshal(content, &records); err != nil {
		return nil, err
	}
	for index := range records {
		redactDeletedScript(&records[index], state)
	}
	return json.Marshal(records)
}
