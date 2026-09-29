package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

const scriptCallTimeout = scriptprotocol.EvaluateHostTimeout
const scriptToolsTimeout = scriptprotocol.ExecuteTransportTimeout

type scriptExecutor interface {
	Execute(context.Context, scriptclient.Request, []scriptclient.Tool, scriptclient.Callback) (json.RawMessage, error)
}

// ScriptHost owns one bounded VM run and its durable admission. The worker
// receives names and serialized input only; all live authority stays here.
type ScriptHost struct {
	Worker           ScriptEvaluator
	Store            ScriptStore
	Registry         ScriptRegistry
	ReadLimitError   error
	RefreshKnowledge func(context.Context, string, knowledge.Command) error
}

func (s ScriptHost) AddContext(ctx context.Context, owner string, updateID int64, input *agent.Input) error {
	if err := s.recoverKnowledgeCalls(ctx, owner, updateID); err != nil {
		return err
	}
	if err := s.drainKnowledgeRefresh(ctx, owner, updateID); err != nil {
		return err
	}
	records, err := s.Store.Records(ctx, owner, updateID)
	if err != nil {
		return err
	}
	input.Script = s.projectContext(records, s.Worker != nil)
	input.Script.UpdateID = updateID
	input.Script.ReadAuthorities, err = ScriptReadAuthorities(owner, records)
	return err
}

func (s ScriptHost) projectContext(records []ScriptRecord, available bool) *agent.ScriptContext {
	result := &agent.ScriptContext{
		Available: available,
		Remaining: agent.MaxScriptRuns - len(records),
		Runs:      make([]agent.ScriptRun, 0, len(records)),
	}
	for _, record := range records {
		for _, call := range record.Calls {
			record.Run.Calls = append(record.Run.Calls, ScriptCallProjection(call.Outcome))
		}
		run := record.Run
		run.PassRedacted = record.PassRedacted
		result.Runs = append(result.Runs, run)
	}
	if !available {
		result.Remaining = 0
	}
	return result
}

func (s ScriptHost) Perform(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.ScriptProposal,
	input *agent.Input,
) (resultErr error) {
	ctx, diagnostic := observability.StartAgentCode(
		ctx,
		observability.AgentEvent{
			Phase:      scriptDiagnosticPhase,
			Operation:  "js.run",
			InputBytes: len(p.InputJSON),
		},
		p.Code,
	)
	defer func() { diagnostic.Finish(resultErr) }()
	if s.Worker == nil || input.Script == nil || input.Script.Remaining <= 0 {
		diagnostic.Outcome("limited", "budget")
		return errors.New("script tool unavailable or exhausted")
	}
	if err := agent.ValidateScript(p); err != nil {
		diagnostic.Outcome("invalid", "invalid_input")
		return err
	}
	authorities, err := scriptInputAuthorities(input)
	if err != nil {
		return err
	}
	index, err := s.Store.ReserveRun(
		ctx,
		owner,
		updateID,
		p,
		input.HistoryGeneration,
		input.Registration,
		authorities,
		InputPrivateHistory(input),
	)
	if err != nil {
		return err
	}
	run, err := s.evaluateTools(ctx, owner, updateID, index, p, input)
	if errors.Is(err, errScriptRegistrationCommitted) {
		return s.AddContext(ctx, owner, updateID, input)
	}
	if err != nil {
		return err
	}
	if err = s.recoverKnowledgeCalls(ctx, owner, updateID); err != nil {
		return err
	}
	recordScriptResult(diagnostic, run)
	records, err := s.Store.CompleteRun(ctx, owner, updateID, index, run)
	if s.Store.StaleError != nil && errors.Is(err, s.Store.StaleError) {
		owned, receiptErr := s.ownPrivateDeletion(ctx, owner, updateID, index)
		if receiptErr != nil {
			return receiptErr
		}
		if owned {
			return s.AddContext(ctx, owner, updateID, input)
		}
		projected, projectionErr := s.projectRetiredMemory(ctx, owner, updateID, index, input, err)
		if projectionErr != nil {
			return projectionErr
		}
		if projected {
			return nil
		}
	}
	if err != nil {
		return err
	}
	input.Script = s.projectContext(records, true)
	input.Script.UpdateID = updateID
	return s.drainKnowledgeRefresh(ctx, owner, updateID)
}

func (s ScriptHost) evaluate(ctx context.Context, p agent.ScriptProposal) (agent.ScriptRun, error) {
	run := agent.ScriptRun{Code: p.Code}
	callCtx, cancel := context.WithTimeout(ctx, scriptCallTimeout)
	defer cancel()
	result, err := s.Worker.Evaluate(callCtx, scriptclient.Request{Code: p.Code, Input: json.RawMessage(p.InputJSON)})
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

func (s ScriptHost) evaluateTools(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	p agent.ScriptProposal,
	input *agent.Input,
) (agent.ScriptRun, error) {
	executor, ok := s.Worker.(scriptExecutor)
	if !ok {
		return s.evaluate(ctx, p)
	}
	run := agent.ScriptRun{Code: p.Code}
	// Authenticate before exposing even the ordinary-user tool catalog.
	tools, err := s.initialTools(ctx, owner)
	if err != nil {
		return run, err
	}
	callCtx, cancel := context.WithTimeout(ctx, scriptToolsTimeout)
	defer cancel()
	callCtx, stopSource := context.WithCancelCause(callCtx)
	defer stopSource(nil)
	scope := scriptRunScope(tools)
	result, err := executor.Execute(
		callCtx,
		scriptclient.Request{Code: p.Code, Input: json.RawMessage(p.InputJSON)},
		scriptBindings(tools),
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			ctx = context.WithValue(ctx, scriptRunScopeKey{}, scope)
			ctx = context.WithValue(ctx, scriptSourceStopKey{}, stopSource)
			return s.Call(ctx, owner, updateID, index, call, input)
		},
	)
	if ctx.Err() != nil {
		return run, ctx.Err()
	}
	if errors.Is(context.Cause(callCtx), errScriptRegistrationCommitted) {
		return run, errScriptRegistrationCommitted
	}
	if errors.Is(context.Cause(callCtx), ErrScriptLedgerConflict) {
		return run, ErrScriptLedgerConflict
	}
	if errors.Is(context.Cause(callCtx), s.Store.StaleError) {
		run.Result = json.RawMessage(`{"omitted":true,"reason":"source_changed","restart":true}`)
		return run, nil
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

func (s ScriptHost) initialTools(ctx context.Context, owner string) ([]scriptclient.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, scriptprotocol.HostCallTimeout)
	defer cancel()
	return s.Registry.Available(ctx, owner)
}

const scriptTimeout = "timeout"
const scriptCanceled = "canceled"
const scriptInvalidResult = "invalid_result"
const scriptResultLimit = "result_limit"
const scriptExecutionError = "script_error"

func scriptFailure(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return scriptTimeout
	}
	if errors.Is(err, scriptclient.ErrResultLimit) {
		return scriptResultLimit
	}
	if errors.Is(err, context.Canceled) {
		return scriptCanceled
	}
	if errors.Is(err, scriptclient.ErrInvalidResult) || errors.Is(err, scriptclient.ErrInvalidRequest) {
		return scriptInvalidResult
	}
	if errors.Is(err, scriptclient.ErrExecution) {
		return scriptExecutionError
	}
	return "execution_failed"
}

func recordScriptResult(span *observability.AgentSpan, run agent.ScriptRun) {
	var items []json.RawMessage
	result := bytes.TrimSpace(run.Result)
	list := len(result) > 0 && result[0] == '[' && json.Unmarshal(result, &items) == nil
	span.Result(len(run.Result), len(items), list && len(items) == 0 && run.Error == "")
	recordScriptOutcome(span, run.Error)
}

func scriptResultError(result json.RawMessage) string {
	if !json.Valid(result) || !utf8.Valid(result) {
		return scriptInvalidResult
	}
	serialized, err := json.Marshal(result)
	if err != nil {
		return scriptInvalidResult
	}
	if len(serialized) > agent.MaxScriptResultBytes {
		return scriptResultLimit
	}
	return ""
}

func recordScriptOutcome(span *observability.AgentSpan, code string) {
	switch code {
	case "":
	case scriptTimeout:
		span.Outcome("timeout", "timeout")
	case scriptCanceled:
		span.Outcome("canceled", "canceled")
	case scriptInvalidResult:
		span.Outcome("invalid", "invalid_input")
	case scriptResultLimit:
		span.Outcome("limited", "result_limit")
	case scriptExecutionError:
		span.Outcome("error", "execution_failed")
	default:
		span.Outcome("interrupted", "unavailable")
	}
}
