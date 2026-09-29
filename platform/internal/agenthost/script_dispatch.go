package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptDiscoveryList = "$list"
const sourceAuthorityLimit = "source_authority_limit"

type scriptSourceStopKey struct{}

func (s ScriptHost) stopStale(ctx context.Context, err error) {
	if !errors.Is(err, s.Store.StaleError) && !errors.Is(err, ErrScriptLedgerConflict) &&
		!errors.Is(err, errScriptRegistrationCommitted) {
		return
	}
	if stop, ok := ctx.Value(scriptSourceStopKey{}).(context.CancelCauseFunc); ok {
		stop(err)
	}
}
func (s ScriptHost) Call(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	call scriptclient.ToolCall,
	input *agent.Input,
) (json.RawMessage, error) {
	if call.Name == "profile.set" {
		if err := s.Store.MarkPrivateProfile(ctx, owner, updateID, index); err != nil {
			s.stopStale(ctx, err)
			return nil, err
		}
	}
	operation := call.Name
	if call.Name == scriptDiscoveryList || call.Name == "$help" {
		operation = "discovery"
	}
	ctx, diagnostic := observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: "tool", Operation: operation, InputBytes: len(call.Arguments)})
	output, err := s.callObserved(ctx, owner, updateID, index, call, input, diagnostic)
	s.stopStale(ctx, err)
	ClearUncommittedModernOrderRead(input, call.Name, err == nil)
	count, empty := ScriptToolResultMetadata(call.Name, output)
	diagnostic.Result(len(output), count, empty && err == nil)
	diagnostic.Finish(err)
	return output, err
}

func (s ScriptHost) callObserved(ctx context.Context, owner string, updateID int64, index int,
	call scriptclient.ToolCall, input *agent.Input, diagnostic *observability.AgentSpan) (json.RawMessage, error) {
	if call.Name == scriptDiscoveryList || call.Name == "$help" {
		return s.discover(ctx, owner, call)
	}

	entry, err := s.Registry.Resolve(ctx, owner, call.Name)
	if err != nil {
		diagnostic.Outcome("denied", "unavailable")
		return nil, errors.New("tool unavailable")
	}
	stageCtx, stage := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.source.admit"},
	)
	sourceErr := s.Store.AdmitSource(stageCtx, owner, updateID, index)
	stage.Finish(sourceErr)
	if sourceErr != nil {
		if response := ScriptSourceFailure(sourceErr, s.Store.StaleError); response != nil {
			s.stopStale(ctx, sourceErr)
			diagnostic.Outcome("interrupted", "source_changed")
			return response, nil
		}
		return nil, sourceErr
	}
	stageCtx, stage = observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.call.prepare"},
	)
	record, err := entry.Prepare(stageCtx, owner, updateID, call, *input)
	stage.Finish(err)
	if err != nil {
		// Preparation also reads fresh domain state; an error is not proof of bad model input.
		diagnostic.Outcome("error", "unavailable")
		return nil, errors.New("tool unavailable or invalid arguments")
	}
	stageCtx, stage = observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.call.admit"},
	)
	sequence, err := s.Store.AdmitCall(stageCtx, owner, updateID, index, &record)
	stage.Finish(err)
	if err != nil {
		if response := ScriptSourceFailure(err, s.Store.StaleError); response != nil {
			s.stopStale(ctx, err)
			return response, nil
		}
		return nil, err
	}
	stageCtx, stage = observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.call.execute"},
	)
	result, err := entry.Execute(stageCtx, owner, call, record, input)
	stage.Finish(err)
	result, outcomeError, err := NormalizeScriptOutcome(result, err, diagnostic, s.Store.StaleError, s.ReadLimitError)
	if err != nil {
		var problem *core.ProblemError
		if errors.As(err, &problem) && problem.Status < 500 {
			diagnostic.Outcome("denied", "unavailable")
			record.Outcome.Error = "denied"
			if saveErr := s.Store.CompleteCall(ctx, owner, updateID, index, sequence, record); saveErr != nil {
				return nil, saveErr
			}
		}
		return nil, errors.New("tool execution failed; inspect host call outcomes")
	}
	visible, err := s.EncodeResult(&record, result, outcomeError, entry.ResultLimit)
	if err != nil {
		diagnostic.Outcome("limited", "result_limit")
		return nil, err
	}
	err = s.completeObservedCall(ctx, owner, updateID, index, sequence, record)
	if err != nil {
		return nil, err
	}
	return visible, nil
}

// completeObservedCall measures durable completion separately from domain execution.
func (s ScriptHost) completeObservedCall(ctx context.Context, owner string, updateID int64, index, sequence int,
	record ScriptToolRecord) error {
	ctx, span := observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.call.complete"})
	err := s.Store.CompleteCall(ctx, owner, updateID, index, sequence, record)
	span.Finish(err)
	return err
}

func (s ScriptHost) discover(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
) (json.RawMessage, error) {
	if call.Name == scriptDiscoveryList {
		if err := decodeDiscoveryArguments(call.Arguments, &struct{}{}); err != nil {
			return nil, err
		}
		tools, err := s.Registry.Available(ctx, owner)
		if err != nil {
			return nil, errors.New("tool unavailable")
		}
		type summary struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		list := make([]summary, 0)
		for _, tool := range tools {
			list = append(list, summary{Name: tool.Name, Description: tool.Description})
		}
		return json.Marshal(list)
	}
	var args struct {
		Name string `json:"name"`
	}
	if err := decodeDiscoveryArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	entry, err := s.Registry.Resolve(ctx, owner, args.Name)
	if err != nil {
		return nil, err
	}
	return json.Marshal(entry.Descriptor)
}

func (s ScriptHost) EncodeResult(
	record *ScriptToolRecord,
	result any,
	outcomeError string,
	limit int,
) (json.RawMessage, error) {
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	visible, err := json.Marshal(ModelToolEvidence(result))
	if err != nil || len(visible) > limit {
		return nil, errors.New("tool result exceeds limit; inspect current state")
	}
	record.Outcome.Result = body
	record.Outcome.Error = outcomeError
	record.KnowledgeRefreshPending = knowledgeRefreshRequired(*record) && outcomeError == ""
	evidence, evidenceErr := ScriptCallResultAuthorities(*record)
	if evidenceErr != nil {
		if !errors.Is(evidenceErr, readsource.ErrLimit) {
			return nil, evidenceErr
		}
		visible = ScriptSourceFailure(evidenceErr, s.Store.StaleError)
		record.Outcome.Error = sourceAuthorityLimit
		evidence = []readsource.Authority{}
	}
	record.ResultAuthorities = evidence
	record.Outcome.Result = visible
	return visible, nil
}
func decodeDiscoveryArguments(raw json.RawMessage, target any) error {
	if len(raw) == 0 || len(raw) > agent.MaxScriptInputBytes || raw[0] != '{' {
		return errors.New("invalid tool arguments")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid tool arguments")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("invalid tool arguments")
	}
	return nil
}
