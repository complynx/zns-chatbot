package agenthost

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// projectRetiredMemory exposes only the scrubbed outcome of an admitted run.
// It does not complete the rejected write or resume the retired VM.
func (s ScriptHost) projectRetiredMemory(
	ctx context.Context, owner string, updateID int64, index int, input *agent.Input, failure error,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !onlyScriptStale(failure, s.Store.StaleError) {
		return false, nil
	}
	records, err := s.Store.Records(ctx, owner, updateID)
	if err != nil {
		return false, err
	}
	if index < 0 || index >= len(records) || !memoryRetirementProjection(records[index]) {
		return false, nil
	}
	authorities, err := ScriptReadAuthorities(owner, records)
	if err != nil {
		return false, err
	}
	input.Script = s.projectContext(records, s.Worker != nil)
	input.Script.UpdateID = updateID
	input.Script.ReadAuthorities = authorities
	return true, nil
}

func memoryRetirementProjection(record ScriptRecord) bool {
	return record.MemoryRedacted && !record.HistoryRedacted && record.Run.Error == scriptInterrupted
}

// A joined authorization or storage failure must not become a successful projection.
func onlyScriptStale(failure, stale error) bool {
	if failure == nil || stale == nil {
		return false
	}
	if joined, ok := failure.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !onlyScriptStale(cause, stale) {
				return false
			}
		}
		return true
	}
	if wrapped := errors.Unwrap(failure); wrapped != nil {
		return onlyScriptStale(wrapped, stale)
	}
	return errors.Is(failure, stale)
}
