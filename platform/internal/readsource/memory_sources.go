package readsource

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

// memoryEvidence resolves an opaque revision to its stored flat closure. Each
// ancestor must predate the revision, so cycles and forward references fail closed.
func memoryEvidence(ctx context.Context, tx pgx.Tx, ref knowledgeauthority.ReadAuthority) ([]Authority, error) {
	var raw []byte
	var captured time.Time
	err := tx.QueryRow(ctx, `SELECT a.authorities,r.captured_at FROM core.memory_read_authorities a JOIN core.memory_revisions r
 USING(namespace,owner,scope,topic,item_key,source_kind,version)
 WHERE a.namespace=$1 AND a.owner=$2 AND a.scope=$3 AND a.topic=$4 AND a.item_key=$5 AND a.source_kind=$6 AND a.version=$7`, ref.Namespace, ref.Owner, ref.Scope, ref.Topic, ref.Key, ref.SourceKind, ref.Version).
		Scan(&raw, &captured)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Authority{}, nil
	}
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	if len(raw) > MaxAuthorityBytes {
		return nil, ErrLimit
	}
	var refs []Authority
	if err = json.Unmarshal(raw, &refs); err != nil {
		return nil, err
	}
	if refs == nil || !Valid(refs) {
		return nil, ErrLimit
	}
	for _, group := range refs {
		if group.Causal == nil {
			return nil, ErrLimit
		}
		for _, leaf := range group.Causal.Authorities {
			if err = memoryAncestorBefore(ctx, tx, leaf.Knowledge, captured); err != nil {
				return nil, err
			}
		}
	}
	return refs, nil
}

func memoryAncestorBefore(
	ctx context.Context,
	tx pgx.Tx,
	ref knowledgeauthority.ReadAuthority,
	captured time.Time,
) error {
	var earlier bool
	var err error
	switch ref.Kind {
	case knowledgeauthority.DerivedMemory:
		err = tx.QueryRow(ctx, `SELECT captured_at<$8 FROM core.memory_revisions WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND source_kind=$6 AND version=$7`, ref.Namespace, ref.Owner, ref.Scope, ref.Topic, ref.Key, ref.SourceKind, ref.Version, captured).
			Scan(&earlier)
	case knowledgeauthority.DerivedProposal:
		err = tx.QueryRow(ctx, `SELECT created_at<$4 FROM core.knowledge_proposals WHERE id=$1 AND owner=$2 AND scope=$3`, ref.ProposalID, ref.Owner, ref.Scope, captured).
			Scan(&earlier)
	default:
		return nil
	}
	// Missing ancestors are denied by the ordinary leaf check; never turn a
	// deleted source into a loader error that obscures its stale result.
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !earlier {
		return ErrLimit
	}
	return nil
}
