package bot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

const historyDeleted = "history_deleted"

// Only a durably saved terminal plan permits the inbox to finish a stale update.
var errHistoryPlanTerminal = fmt.Errorf("terminal history plan: %w", errScriptReadStale)

// Script output may transform history, so invalidate the whole result.
func redactHistoryScript(record *scriptRecord, generation int64) bool {
	if record.HistoryGeneration == generation && !record.HistoryRedacted {
		return false
	}
	record.HistoryGeneration, record.HistoryRedacted = generation, true
	record.Request = agent.ScriptProposal{}
	record.Run.Code, record.Run.Error = "", historyDeleted
	record.Run.Result = json.RawMessage(`{"omitted":true,"reason":"history_deleted"}`)
	record.Run.Calls = nil
	for index := range record.Calls {
		record.Calls[index].Outcome.Result = nil
		record.Calls[index].Outcome.Error = historyDeleted
		if record.Calls[index].Memory != nil {
			record.Calls[index].Memory.Text = ""
		}
	}
	return true
}

func redactHistoryPages(pages []conversation.Page, generation int64) {
	for index := range pages {
		if pages[index].Generation != generation {
			pages[index] = conversation.Page{
				Events:     []conversation.Event{},
				Generation: generation,
				Error:      historyDeleted,
			}
		}
	}
}

func (b *Bot) reauthorizeHistoryContext(ctx context.Context, owner string, input *agent.Input) error {
	if input.Conversation == nil && input.Script == nil {
		return nil
	}
	generation, err := b.API.historyGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if input.HistoryGeneration != generation {
		input.History = nil
		if input.Conversation != nil {
			input.Conversation.Summary = conversation.Summary{}
			input.Conversation.Gap = true
			redactHistoryPages(input.Conversation.Reads, generation)
		}
		if input.Script != nil {
			for index := range input.Script.Runs {
				input.Script.Runs[index] = agent.ScriptRun{Error: historyDeleted}
			}
		}
		input.HistoryGeneration = generation
	}
	return nil
}

// Reconcile old ledgers in bounded batches; current reads are independently fenced.
func (b *Bot) reconcileHistoryCaches(ctx context.Context, owner string, generation int64) error {
	if generation == 0 {
		return nil
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(
		ctx,
		`SELECT update_id,kind,content FROM bot.interactions WHERE owner=$1 AND kind IN ('history_reads','script_runs')
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(content) r WHERE COALESCE((CASE WHEN kind='script_runs' THEN r->>'history_generation' ELSE r->>'generation' END)::bigint,0)<>$2)
 ORDER BY update_id,kind LIMIT 100 FOR UPDATE SKIP LOCKED`,
		owner,
		generation,
	)
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
		content, redactErr := redactHistoryInteraction(item.kind, item.content, generation)
		if redactErr != nil {
			return redactErr
		}
		if _, err = tx.Exec(
			ctx,
			`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
			owner,
			item.updateID,
			item.kind,
			content,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Preserve a terminal marker rather than deleting the row: retries must not
// regenerate commands whose original execution state may already be committed.
func (b *Bot) validateHistoryPlan(ctx context.Context, owner string, updateID int64, plan cachedPlan) error {
	generation, err := b.API.historyGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if plan.HistoryGeneration == generation && !plan.HistoryRedacted {
		return nil
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	redacted := cachedPlan{HistoryGeneration: generation, HistoryRedacted: true}
	if _, err = tx.Exec(ctx, `INSERT INTO bot.replies(update_id,plan) VALUES($1,$2)
 ON CONFLICT(update_id) DO UPDATE SET plan=excluded.plan`, updateID, redacted); err != nil {
		return err
	}
	// Retain execution receipts, but remove output that later cards could render.
	if _, err = tx.Exec(ctx, `DELETE FROM bot.interactions WHERE owner=$1 AND update_id=$2
 AND kind IN ('reply','orders_reply','knowledge_reply','profile_reply','profile_answer','registration_reply')`, owner, updateID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return errHistoryPlanTerminal
}

// A crash can leave committed script effects before the final plan is saved.
// Deleted input makes that unfinished turn terminal, not a new model request.
func (b *Bot) validateHistoryInteractions(ctx context.Context, owner string, updateID, generation int64) error {
	var stale bool
	err := b.DB.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM bot.interactions i, jsonb_array_elements(i.content) r
 WHERE i.owner=$1 AND i.update_id=$2 AND i.kind IN ('history_reads','script_runs')
 AND (COALESCE((CASE WHEN i.kind='script_runs' THEN r->>'history_generation' ELSE r->>'generation' END)::bigint,0)<>$3
 OR r->>'history_redacted'='true' OR r->>'error'='history_deleted'))`, owner, updateID, generation).Scan(&stale)
	if err != nil || !stale {
		return err
	}
	return b.validateHistoryPlan(ctx, owner, updateID, cachedPlan{HistoryRedacted: true})
}

func redactHistoryInteraction(kind string, raw json.RawMessage, generation int64) (json.RawMessage, error) {
	if kind == scriptRunsKind {
		var records []scriptRecord
		if err := json.Unmarshal(raw, &records); err != nil {
			return nil, err
		}
		for index := range records {
			redactHistoryScript(&records[index], generation)
		}
		return json.Marshal(records)
	}
	var pages []conversation.Page
	if err := json.Unmarshal(raw, &pages); err != nil {
		return nil, err
	}
	redactHistoryPages(pages, generation)
	return json.Marshal(pages)
}

func (b *Bot) historyFencedPlan(ctx context.Context, owner string, input agent.Input) (agent.Plan, error) {
	plan, err := b.diagnosticPlan(ctx, input)
	if err != nil {
		return plan, err
	}
	return plan, b.API.checkHistoryGeneration(ctx, owner, input.HistoryGeneration)
}
