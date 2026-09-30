package knowledge

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Receipts remain durable, but their derived bodies retain revision tombstones.
// A denied reader does not revoke the underlying content for other readers.
func authorizeMemoryReplay(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	result *Result,
	inputs []readsource.Authority,
	source *readsource.Derivation,
	revoked bool,
) (bool, error) {
	outputs, err := memoryResultAuthorities(*result)
	if err != nil {
		return false, err
	}
	if len(outputs) == 0 && len(inputs) == 0 && source == nil && !revoked {
		return false, nil
	}
	// Both persisted carriers retain their full independent bounds. Output refs
	// add only revision/proposal leaves to the source prelude already locked.
	inputValidity, err := readsource.LockValidity(ctx, tx, actor, inputs)
	if err != nil {
		return false, err
	}
	outputValidity, err := readsource.LockValidity(ctx, tx, actor, outputs)
	if err != nil {
		return false, err
	}
	outputLost := slices.Contains(outputValidity.Origin, false)
	originLost := revoked || outputLost || slices.Contains(inputValidity.Origin, false)
	denied := slices.Contains(inputValidity.Reader, false) || slices.Contains(outputValidity.Reader, false)
	if source != nil {
		err = fence.LockGeneration(ctx, tx, actor, source.Generation)
		if err != nil {
			problem, ok := errors.AsType[*core.ProblemError](err)
			if !ok || problem.Code != "history_stale" {
				return false, err
			}
			originLost = true
		}
	}
	if outputLost {
		if err = revokeMemoryResult(ctx, tx, actor, *result); err != nil {
			return false, err
		}
	}
	if originLost || denied {
		scrubMemoryResult(result)
	}
	return originLost, projectReplayEvidence(actor, result)
}

func memoryResultAuthorities(result Result) ([]readsource.Authority, error) {
	groups := [][]readsource.Authority{result.ReadAuthorities}
	if result.Fact != nil {
		groups = append(groups, result.Fact.ReadAuthorities)
	}
	if result.Memo != nil {
		groups = append(groups, result.Memo.ReadAuthorities)
	}
	if result.Document != nil {
		groups = append(groups, result.Document.ReadAuthorities)
	}
	if result.Proposal != nil {
		groups = append(groups, result.Proposal.ReadAuthorities)
	}
	return readsource.Merge(groups...)
}

func revokeMemoryResult(ctx context.Context, tx pgx.Tx, actor string, result Result) error {
	if ref, ok := resultMemoryReference(result); ok {
		entry := MemoryEntry{
			Namespace:  ref.Namespace,
			Event:      ref.Event,
			Topic:      ref.Topic,
			Key:        ref.Key,
			SourceKind: ref.SourceKind,
			Version:    ref.Version,
		}
		if err := revokeMemoryCausal(ctx, tx, actor, entry); err != nil {
			return err
		}
	}
	if result.Proposal != nil {
		_, err := tx.Exec(
			ctx,
			`UPDATE core.knowledge_proposal_authorities SET revoked=true WHERE proposal_id=$1`,
			result.Proposal.ID,
		)
		return core.DatabaseOperationError(err)
	}
	return nil
}
