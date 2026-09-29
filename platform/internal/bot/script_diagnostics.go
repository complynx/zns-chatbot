package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

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
