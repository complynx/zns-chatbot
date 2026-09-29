package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type ScriptEvaluator interface {
	Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error)
}

const scriptRunsKind = "script_runs"
const scriptCallTimeout = 3 * time.Second

type scriptExecutor interface {
	Execute(context.Context, scriptclient.Request, []scriptclient.Tool, scriptclient.Callback) (json.RawMessage, error)
}

type scriptRecord struct {
	HistoryGeneration int64                         `json:"history_generation"`
	HistoryRedacted   bool                          `json:"history_redacted,omitempty"`
	PrivateProfile    bool                          `json:"private_profile,omitempty"`
	MemoryRedacted    bool                          `json:"memory_redacted,omitempty"`
	MemoryState       knowledge.MemoryDeletionState `json:"memory_state"`
	Calls             []scriptToolRecord            `json:"calls,omitempty"`
	Request           agent.ScriptProposal          `json:"request"`
	Run               agent.ScriptRun               `json:"run"`
}

func (b *Bot) addScriptContext(ctx context.Context, owner string, updateID int64, input *agent.Input) error {
	records, err := b.scriptRecords(ctx, owner, updateID)
	if err != nil {
		return err
	}
	input.Script = scriptContext(records, b.Scripts != nil)
	input.Script.UpdateID = updateID
	return nil
}

func scriptContext(records []scriptRecord, available bool) *agent.ScriptContext {
	result := &agent.ScriptContext{
		Available: available,
		Remaining: agent.MaxScriptRuns - len(records),
		Runs:      make([]agent.ScriptRun, 0, len(records)),
	}
	for _, record := range records {
		for _, call := range record.Calls {
			record.Run.Calls = append(record.Run.Calls, scriptCallProjection(call.Outcome))
		}
		result.Runs = append(result.Runs, record.Run)
	}
	if !available {
		result.Remaining = 0
	}
	return result
}

func (b *Bot) scriptRecords(ctx context.Context, owner string, updateID int64) ([]scriptRecord, error) {
	generation, generationErr := b.API.historyGeneration(ctx, owner)
	if generationErr != nil {
		return nil, generationErr
	}
	if generationErr = b.reconcileHistoryCaches(ctx, owner, generation); generationErr != nil {
		return nil, generationErr
	}
	state, stateErr := b.API.memoryDeletions(ctx, owner)
	if stateErr != nil {
		return nil, stateErr
	}
	if stateErr = b.reconcileMemoryScripts(ctx, owner, state); stateErr != nil {
		return nil, stateErr
	}
	var records []scriptRecord
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3`, owner, updateID, scriptRunsKind).
		Scan(&records)
	if errors.Is(err, pgx.ErrNoRows) {
		return []scriptRecord{}, nil
	}
	for index := range records {
		redactProfileScript(&records[index])
		redactDeletedScript(&records[index], state)
		redactHistoryScript(&records[index], generation)
	}
	return records, err
}

func (b *Bot) performScript(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.ScriptProposal,
	input *agent.Input,
) (resultErr error) {
	ctx, diagnostic := observability.StartAgentCode(ctx,
		observability.AgentEvent{Phase: "script", Operation: "js.run", InputBytes: len(p.InputJSON)}, p.Code)
	defer func() { diagnostic.Finish(resultErr) }()
	if b.Scripts == nil || input.Script == nil || input.Script.Remaining <= 0 {
		diagnostic.Outcome("limited", "budget")
		return errors.New("script tool unavailable or exhausted")
	}
	if err := agent.ValidateScript(p); err != nil {
		diagnostic.Outcome("invalid", "invalid_input")
		return err
	}
	index, err := b.reserveScript(ctx, owner, updateID, p, input.HistoryGeneration)
	if err != nil {
		return err
	}
	run, err := b.evaluateScriptTools(ctx, owner, updateID, index, p, input)
	if err != nil {
		return err
	}
	recordScriptResult(diagnostic, run)
	records, err := b.finishScript(ctx, owner, updateID, index, run)
	if err != nil {
		return err
	}
	input.Script = scriptContext(records, true)
	input.Script.UpdateID = updateID
	return nil
}

func (b *Bot) evaluateScript(ctx context.Context, p agent.ScriptProposal) (agent.ScriptRun, error) {
	run := agent.ScriptRun{Code: p.Code}
	callCtx, cancel := context.WithTimeout(ctx, scriptCallTimeout)
	defer cancel()
	result, err := b.Scripts.Evaluate(callCtx, scriptclient.Request{Code: p.Code, Input: json.RawMessage(p.InputJSON)})
	if ctx.Err() != nil {
		return run, ctx.Err()
	}
	if err != nil || callCtx.Err() != nil {
		run.Error = scriptFailure(callCtx, err)
		return run, nil
	}
	if run.Error = scriptResultError(result); run.Error != "" {
		return run, nil
	}
	run.Result = result
	return run, nil
}

func (b *Bot) performReadTool(
	ctx context.Context,
	owner string,
	updateID int64,
	plan agent.Plan,
	input *agent.Input,
) (bool, error) {
	if plan.ScriptAction != nil {
		return true, b.performScript(ctx, owner, updateID, *plan.ScriptAction, input)
	}
	if agent.IsKnowledgeRead(plan.KnowledgeAction) {
		return true, b.performKnowledgeRead(ctx, owner, updateID, *plan.KnowledgeAction, input)
	}
	return false, nil
}

func scriptLock(ctx context.Context, tx pgx.Tx, owner string, updateID int64) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"script:"+owner+":"+strconv.FormatInt(updateID, 10),
	)
	return err
}

// Reserve before worker I/O. A process interruption leaves a visible consumed
// slot, so replay cannot run an unbounded number of scripts under one update.
func (b *Bot) reserveScript(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.ScriptProposal,
	generation int64,
) (int, error) {
	current, generationErr := b.API.historyGeneration(ctx, owner)
	if generationErr != nil {
		return 0, generationErr
	}
	if current != generation {
		return 0, errScriptReadStale
	}
	state, err := b.API.memoryDeletions(ctx, owner)
	if err != nil {
		return 0, err
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = scriptLock(ctx, tx, owner, updateID); err != nil {
		return 0, err
	}
	var records []scriptRecord
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, scriptRunsKind).
		Scan(&records)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if len(records) >= agent.MaxScriptRuns {
		return 0, errors.New("script budget exhausted")
	}
	index := len(records)
	records = append(
		records,
		scriptRecord{
			HistoryGeneration: generation,
			MemoryState:       state,
			Request:           p,
			Run:               agent.ScriptRun{Code: p.Code, Error: scriptInterrupted},
		},
	)
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, scriptRunsKind, records)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func (b *Bot) finishScript(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	run agent.ScriptRun,
) ([]scriptRecord, error) {
	state, stateErr := b.API.memoryDeletions(ctx, owner)
	if stateErr != nil {
		return nil, stateErr
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = scriptLock(ctx, tx, owner, updateID); err != nil {
		return nil, err
	}
	var records []scriptRecord
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3 FOR UPDATE`, owner, updateID, scriptRunsKind).
		Scan(&records); err != nil {
		return nil, err
	}
	if index >= len(records) {
		return nil, errors.New("script reservation missing")
	}
	generation, generationErr := b.API.historyGeneration(ctx, owner)
	if generationErr != nil {
		return nil, generationErr
	}
	completeScriptRecord(&records[index], run, state, generation)
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$4 WHERE owner=$1 AND update_id=$2 AND kind=$3`,
		owner,
		updateID,
		scriptRunsKind,
		records,
	)
	if err != nil {
		return nil, err
	}
	return records, tx.Commit(ctx)
}

func completeScriptRecord(
	record *scriptRecord,
	run agent.ScriptRun,
	state knowledge.MemoryDeletionState,
	generation int64,
) {
	record.Run = run
	// Terminal requests are never re-executed. Retain effect identities and
	// evidence, without keeping extra copies of arbitrary source/input text.
	record.Request = agent.ScriptProposal{}
	record.Run.Code = ""
	for callIndex := range record.Calls {
		if record.Calls[callIndex].Memory != nil {
			record.Calls[callIndex].Memory.Text = ""
		}
	}
	redactProfileScript(record)
	redactDeletedScript(record, state)
	redactHistoryScript(record, generation)
}
