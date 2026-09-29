package knowledge

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// CommandReceipt recovers only a previously committed operation. It never
// executes the command and never returns a cached derived body without checking
// its original input authority and current revision tombstones.
func (s Service) CommandReceipt(
	ctx context.Context,
	actor string,
	c Command,
	source readsource.Derivation,
) (Result, bool, error) {
	if err := validate(c, false); err != nil {
		return Result{}, false, err
	}
	if !source.Valid() {
		return Result{}, false, errors.New("invalid derivation")
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Result{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	proposal := proposalCausalRecord{}
	refs := source.Authorities
	if c.Name == Review {
		proposal, err = loadProposalCausal(ctx, tx, c.ProposalID)
		if err != nil {
			return Result{}, false, err
		}
		refs, err = readsource.Merge(refs, proposal.refs)
		if err != nil {
			return Result{}, false, err
		}
	}
	if err = lockKnowledgeCommand(ctx, tx, actor, c, refs, true); err != nil {
		return Result{}, false, err
	}
	encoded, err := knowledgeOperationBytes(c, nil, &source)
	if err != nil {
		return Result{}, false, err
	}
	keyHash := digest([]byte(c.Key))
	result, found, err := replay(ctx, tx, actor, keyHash, digest(encoded))
	if err != nil || !found {
		return result, found, err
	}
	terminal, err := authorizeMemoryReplay(ctx, tx, actor, &result, refs, &source, proposal.revoked)
	if err != nil {
		return Result{}, false, err
	}
	if terminal {
		if _, err = tx.Exec(
			ctx,
			`UPDATE core.knowledge_operations SET result=$3 WHERE actor=$1 AND key_hash=$2`,
			actor,
			keyHash,
			result,
		); err != nil {
			return Result{}, false, err
		}
	}
	if err = verifyPrivateDeletion(ctx, tx, actor, c, source, &result); err != nil {
		return Result{}, false, err
	}
	return result, true, tx.Commit(ctx)
}
