package agenthost

import (
	"context"

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
