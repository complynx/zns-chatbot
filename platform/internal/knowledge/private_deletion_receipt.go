package knowledge

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// PrivateDeletionWitness records only the generations changed by this committed
// memo or document deletion. Current is recomputed by CommandReceipt.
type PrivateDeletionWitness struct {
	SourceKind    string              `json:"source_kind"`
	BeforeHistory int64               `json:"before_history"`
	AfterHistory  int64               `json:"after_history"`
	BeforeMemory  MemoryDeletionState `json:"before_memory"`
	AfterMemory   MemoryDeletionState `json:"after_memory"`
	Current       bool                `json:"current,omitempty"`
}

func privateDeletionState(ctx context.Context, tx pgx.Tx, actor string) (int64, MemoryDeletionState, error) {
	history, err := fence.CurrentGeneration(ctx, tx, actor)
	if err != nil {
		return 0, MemoryDeletionState{}, err
	}
	var memory MemoryDeletionState
	err = tx.QueryRow(ctx, memoryDeletionStateQuery, actor).Scan(&memory.PrivateGeneration, &memory.SharedGeneration)
	return history, memory, core.DatabaseOperationError(err)
}

func privateDeletionIdentity(command Command) (string, string) {
	switch command.Name {
	case MemoDelete:
		return memoryPrivateTopic(command.FactKey), MemoryMemoKind
	case DocumentDelete:
		return command.Topic, MemoryDocumentKind
	default:
		return "", ""
	}
}

func beginPrivateDeletion(ctx context.Context, tx pgx.Tx, actor string, command Command,
	source *readsource.Derivation) (PrivateDeletionWitness, error) {
	_, kind := privateDeletionIdentity(command)
	if kind == "" || source == nil || source.Generation == nil {
		return PrivateDeletionWitness{}, nil
	}
	history, memory, err := privateDeletionState(ctx, tx, actor)
	if err != nil {
		return PrivateDeletionWitness{}, err
	}
	return PrivateDeletionWitness{SourceKind: kind, BeforeHistory: history, BeforeMemory: memory}, nil
}

func finishPrivateDeletion(ctx context.Context, tx pgx.Tx, actor string, witness PrivateDeletionWitness,
	result *Result) error {
	if witness.SourceKind == "" {
		return nil
	}
	history, memory, err := privateDeletionState(ctx, tx, actor)
	if err != nil {
		return err
	}
	witness.AfterHistory, witness.AfterMemory = history, memory
	result.PrivateDeletion = &witness
	return nil
}

// Check a detached source only. The original command admission remains the
// canonical replay identity; this copy is neither persisted nor projected.
func privateDeletionSource(actor string, command Command, source readsource.Derivation,
	witness PrivateDeletionWitness) (readsource.Derivation, bool) {
	_, kind := privateDeletionIdentity(command)
	if kind == "" || witness.SourceKind != kind ||
		source.Generation == nil || *source.Generation != witness.BeforeHistory ||
		witness.AfterHistory < witness.BeforeHistory ||
		witness.AfterMemory.PrivateGeneration <= witness.BeforeMemory.PrivateGeneration ||
		witness.AfterMemory.SharedGeneration != witness.BeforeMemory.SharedGeneration {
		return readsource.Derivation{}, false
	}
	result := source.Clone()
	*result.Generation = witness.AfterHistory
	result.Authorities = privateDeletionLeaves(actor, command, result.Authorities, witness)
	return result, true
}

func privateDeletionLeaves(actor string, command Command, refs []readsource.Authority,
	witness PrivateDeletionWitness) []readsource.Authority {
	topic, kind := privateDeletionIdentity(command)
	result := make([]readsource.Authority, 0, len(refs))
	for _, ref := range refs {
		if ref.Causal != nil {
			causal := ref.Causal
			if causal.Actor == actor && causal.Generation != nil && *causal.Generation == witness.BeforeHistory {
				*causal.Generation = witness.AfterHistory
				causal.Authorities = privateDeletionLeaves(actor, command, causal.Authorities, witness)
			}
			result = append(result, ref)
			continue
		}
		authority := &ref.Knowledge
		if authority.Kind == knowledgeauthority.DerivedMemory && authority.Namespace == MemoryPrivate &&
			authority.Owner == actor && authority.Key == command.FactKey && authority.Topic == topic &&
			authority.SourceKind == kind && authority.Version == command.Version {
			continue
		}
		if authority.Kind == knowledgeauthority.PrivateMemory &&
			authority.Generation == witness.BeforeMemory.PrivateGeneration {
			authority.Generation = witness.AfterMemory.PrivateGeneration
		}
		result = append(result, ref)
	}
	return result
}

func privateDeletionResult(command Command, result Result) bool {
	switch command.Name {
	case MemoDelete:
		return result.Memo != nil && !result.Memo.Active && result.Memo.Key == command.FactKey &&
			result.Memo.Version == command.Version+1
	case DocumentDelete:
		return result.Document != nil && !result.Document.Active && result.Document.Key == command.FactKey &&
			result.Document.Topic == command.Topic && result.Document.Version == command.Version+1
	default:
		return false
	}
}

func currentPrivateDeletion(ctx context.Context, tx pgx.Tx, actor string, command Command) (bool, error) {
	var current bool
	var err error
	switch command.Name {
	case MemoDelete:
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.knowledge_memos
 WHERE owner=$1 AND memo_key=$2 AND version=$3 AND NOT active)`,
			actor, command.FactKey, command.Version+1).Scan(&current)
	case DocumentDelete:
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.memory_documents
 WHERE owner=$1 AND topic=$2 AND document_key=$3 AND version=$4 AND NOT active)`,
			actor, command.Topic, command.FactKey, command.Version+1).Scan(&current)
	}
	return current, core.DatabaseOperationError(err)
}

func verifyPrivateDeletion(ctx context.Context, tx pgx.Tx, actor string, command Command,
	source readsource.Derivation, result *Result) error {
	witness := result.PrivateDeletion
	if witness == nil {
		return nil
	}
	witness.Current = false
	if !privateDeletionResult(command, *result) {
		return nil
	}
	// Expand before omitting the deleted revision: its opaque leaf can carry
	// independent origin permissions that must still be checked.
	expanded, err := readsource.ExpandProposalSources(ctx, tx, source.Authorities)
	if err != nil {
		return err
	}
	validationSource := source.Clone()
	validationSource.Authorities = expanded
	check, valid := privateDeletionSource(actor, command, validationSource, *witness)
	if !valid {
		return nil
	}
	history, memory, err := privateDeletionState(ctx, tx, actor)
	if err != nil {
		return err
	}
	if history != witness.AfterHistory || memory != witness.AfterMemory {
		return nil
	}
	current, err := currentPrivateDeletion(ctx, tx, actor, command)
	if err != nil || !current {
		return err
	}
	validity, err := readsource.LockValidity(ctx, tx, actor, check.Authorities)
	if err != nil {
		return err
	}
	witness.Current = !slices.Contains(validity.Origin, false) && !slices.Contains(validity.Reader, false)
	return nil
}
