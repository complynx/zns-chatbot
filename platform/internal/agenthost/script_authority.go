package agenthost

import (
	"bytes"
	"context"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// ScriptDomainAuthority supplies current domain decisions. Ledger provenance,
// discovery validation and bounded authority aggregation remain host policy.
type ScriptDomainAuthority interface {
	Generation(context.Context, string) (int64, error)
	MemoryState(context.Context, string) (knowledge.MemoryDeletionState, error)
	SourcesChanged(context.Context, string, []readsource.Authority) (bool, error)
	RegistrationContextChanged(context.Context, string, interaction.PassContextDependency) (bool, error)
	RegistrationCallChanged(context.Context, string, ScriptToolRecord) (bool, error)
	RegistrationSummaryChanged(context.Context, string, agent.ScriptToolResult) (bool, error)
}

type ScriptAuthorization struct{ ScriptDomainAuthority }

// An inspect completion can change its private payload without changing any
// authority. Preserve every prior admission and call identity before sharing
// the one fresh authorization of a local ledger edit.
func sameInspectCarriers(owner string, before, after []ScriptRecord) bool {
	if len(before) != len(after) {
		return false
	}
	for index, old := range before {
		if !sameInspectRecord(owner, old, after[index]) {
			return false
		}
	}
	return true
}

func sameInspectRecord(owner string, old, current ScriptRecord) bool {
	if scriptRetired(old) || scriptRetired(current) || old.PassContext == nil || current.PassContext == nil ||
		old.ReadAuthorities == nil || current.ReadAuthorities == nil || len(current.Calls) < len(old.Calls) {
		return false
	}
	oldRun, oldErr := scriptRunIdentity(old)
	newRun, newErr := scriptRunIdentity(current)
	if oldErr != nil || newErr != nil || !bytes.Equal(oldRun, newRun) {
		return false
	}
	for callIndex, call := range current.Calls {
		if call.Outcome.Name != modernOrdersInspect || call.Source == nil {
			return false
		}
		if callIndex < len(old.Calls) && !sameInspectCall(old.Calls[callIndex], call) {
			return false
		}
	}
	oldRefs, oldBatched, oldErr := scriptRecordAuthorityChecks(owner, old)
	newRefs, newBatched, newErr := scriptRecordAuthorityChecks(owner, current)
	if oldErr != nil || newErr != nil || !oldBatched || !newBatched ||
		!slices.EqualFunc(oldRefs, newRefs, readsource.Equal) {
		return false
	}
	return true
}

func sameInspectCall(previousCall, call ScriptToolRecord) bool {
	if len(previousCall.Outcome.Result) != 0 &&
		(!bytes.Equal(previousCall.Outcome.Result, call.Outcome.Result) || previousCall.Outcome.Error != call.Outcome.Error) {
		return false
	}
	previous, previousErr := scriptCallIdentity(previousCall)
	identity, identityErr := scriptCallIdentity(call)
	return previousErr == nil && identityErr == nil && bytes.Equal(previous, identity)
}

func (policy ScriptAuthorization) AccessChanged(ctx context.Context, owner string, record ScriptRecord) (bool, error) {
	for _, call := range record.Calls {
		if changed, err := policy.RegistrationSummaryChanged(ctx, owner, call.Outcome); changed || err != nil {
			return changed, err
		}
	}
	if record.PassRedacted || record.PassContext == nil || record.ReadAuthorities == nil {
		return true, nil
	}
	if invalid, err := scriptDiscoveryInvalid(record.Calls); invalid || err != nil {
		return invalid, err
	}
	authorities, batched, err := scriptRecordAuthorityChecks(owner, record)
	if err != nil {
		return false, err
	}
	if changed, sourceErr := policy.SourcesChanged(ctx, owner, authorities); changed || sourceErr != nil {
		return changed, sourceErr
	}
	return policy.dependenciesChanged(ctx, owner, record, batched)
}

func (policy ScriptAuthorization) dependenciesChanged(
	ctx context.Context,
	owner string,
	record ScriptRecord,
	batched bool,
) (bool, error) {
	for _, dependency := range record.PassContext {
		if changed, err := policy.RegistrationContextChanged(ctx, owner, dependency); changed || err != nil {
			return changed, err
		}
	}
	for _, call := range record.Calls {
		if ScriptCallHasAuthorities(call) {
			if batched {
				continue
			}
			refs, err := ScriptCallReadAuthorities(owner, call)
			if err != nil {
				return false, err
			}
			if changed, sourceErr := policy.SourcesChanged(ctx, owner, refs); changed || sourceErr != nil {
				return changed, sourceErr
			}
			continue
		}
		if changed, err := policy.RegistrationCallChanged(ctx, owner, call); changed || err != nil {
			return changed, err
		}
	}
	return false, nil
}
