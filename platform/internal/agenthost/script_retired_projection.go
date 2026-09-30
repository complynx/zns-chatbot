package agenthost

import (
	"context"
	"errors"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
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
	if index < 0 || index >= len(records) {
		return false, nil
	}
	owned, err := s.ownPrivateDeletion(ctx, owner, records[index])
	if err != nil {
		return false, err
	}
	if owned {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		return true, s.AddContext(ctx, owner, updateID, input)
	}
	if !memoryRetirementProjection(records[index]) {
		return false, nil
	}
	// PassRedacted is also used for memory retirement. Clear it only on this
	// detached authorization witness; the durable and projected record stay retired.
	witness := memoryProjectionWitness(owner, records[index])
	witness.PassRedacted = false
	changed, err := s.Store.Policy.AccessChanged(ctx, owner, witness)
	if err != nil {
		return false, err
	}
	if changed {
		return false, nil
	}
	if err = ctx.Err(); err != nil {
		return false, err
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

// memoryProjectionWitness checks access to an omitted result, never execution or
// reuse of the old memory. Only deletion epochs are refreshed; domain grants,
// history generations and foreign owners' private epochs remain bound.
func memoryProjectionWitness(owner string, record ScriptRecord) ScriptRecord {
	state := record.MemoryState
	record.ReadAuthorities = memoryProjectionAuthorities(owner, record.ReadAuthorities, state)
	record.Calls = slices.Clone(record.Calls)
	for i := range record.Calls {
		call := &record.Calls[i]
		call.ResultAuthorities = memoryProjectionAuthorities(owner, call.ResultAuthorities, state)
		if call.Source != nil {
			source := call.Source.Clone()
			source.Authorities = memoryProjectionAuthorities(owner, source.Authorities, state)
			call.Source = &source
		}
		if call.MemoryReadState != nil {
			current := state
			call.MemoryReadState = &current
		}
		if call.KnowledgeRead != nil {
			ref := *call.KnowledgeRead
			refreshProjectionEpoch(&ref, state, true)
			call.KnowledgeRead = &ref
		}
	}
	return record
}

func memoryProjectionAuthorities(
	owner string,
	refs []readsource.Authority,
	state knowledge.MemoryDeletionState,
) []readsource.Authority {
	detached := readsource.CloneAuthorities(refs)
	for i := range detached {
		ref := &detached[i]
		if ref.Causal == nil {
			refreshProjectionEpoch(&ref.Knowledge, state, true)
			continue
		}
		for j := range ref.Causal.Authorities {
			refreshProjectionEpoch(&ref.Causal.Authorities[j].Knowledge, state, ref.Causal.Actor == owner)
		}
	}
	return detached
}

func refreshProjectionEpoch(ref *knowledgeauthority.ReadAuthority, state knowledge.MemoryDeletionState, own bool) {
	switch ref.Kind {
	case knowledgeauthority.PrivateMemory:
		if own {
			ref.Generation = state.PrivateGeneration
		}
	case knowledgeauthority.SharedMemory:
		ref.Generation = state.SharedGeneration
	}
}
