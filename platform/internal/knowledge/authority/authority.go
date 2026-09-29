// Package authority owns the knowledge permissions used by private read provenance.
package authority

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const Review = "review"
const PrivateMemory = "private_memory"
const SharedMemory = "shared_memory"
const DerivedMemory = "derived_memory"
const DerivedProposal = "derived_proposal"

type ReadAuthority struct {
	ProposalID int64  `json:"proposal_id,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Key        string `json:"key,omitempty"`
	SourceKind string `json:"source_kind,omitempty"`
	Version    int64  `json:"version,omitempty"`
	Kind       string `json:"kind"`
	Scope      string `json:"scope"`
	Generation int64  `json:"generation"`
}

func (a ReadAuthority) Valid() bool {
	if a.Kind == DerivedProposal {
		return a.ProposalID > 0 && validPart(a.Owner, false) && validPart(a.Scope, true) && a.Namespace == "" &&
			a.Topic == "" &&
			a.Key == "" &&
			a.SourceKind == "" &&
			a.Version == 0 &&
			a.Generation == 0
	}
	if a.ProposalID != 0 {
		return false
	}
	if a.Kind == DerivedMemory {
		return a.validDerivedMemory()
	}
	if a.Namespace != "" || a.Owner != "" || a.Topic != "" || a.Key != "" || a.SourceKind != "" || a.Version != 0 {
		return false
	}
	return (a.Kind == Review && a.Generation == 0 && validPart(a.Scope, true)) ||
		((a.Kind == PrivateMemory || a.Kind == SharedMemory) && a.Scope == "" && a.Generation >= 0)
}

func validPart(value string, empty bool) bool {
	return (empty || value != "") && len(value) <= 200 && !strings.ContainsRune(value, 0)
}

// LockPermission is shared by knowledge operations and derived-source checks.
// The caller locks its actor first and retains this transaction through use.
func LockPermission(ctx context.Context, tx pgx.Tx, actor, scope, permission string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT permission FROM core.knowledge_permissions WHERE scope=$1 AND actor=$2 AND permission=$3 FOR SHARE`, scope, actor, permission).
		Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return err
}

func lockRead(ctx context.Context, tx pgx.Tx, actor string, a ReadAuthority) (bool, error) {
	if !a.Valid() {
		return false, &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_knowledge"}
	}
	if a.Kind == DerivedProposal {
		return lockDerivedProposal(ctx, tx, actor, a)
	}
	if a.Kind == DerivedMemory {
		return lockDerivedRevision(ctx, tx, actor, a)
	}
	if a.Kind == PrivateMemory || a.Kind == SharedMemory {
		epochOwner := actor
		if a.Kind == SharedMemory {
			epochOwner = ""
		}
		var current int64
		err := tx.QueryRow(ctx, `SELECT generation FROM core.memory_deletion_epochs WHERE owner=$1 FOR SHARE`, epochOwner).
			Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			return a.Generation == 0, nil
		}
		return current == a.Generation, err
	}
	err := LockPermission(ctx, tx, actor, a.Scope, Review)
	if p, ok := errors.AsType[*core.ProblemError](err); ok && p.Status == http.StatusForbidden {
		return false, nil
	}
	return err == nil, err
}

// LockReads takes the permanent shared gate before any leaf-specific grant lock.
func LockReads(ctx context.Context, tx pgx.Tx, actor string, refs []ReadAuthority) ([]bool, error) {
	for _, a := range refs {
		if a.Kind == SharedMemory {
			if err := LockSharedGate(ctx, tx); err != nil {
				return nil, err
			}
			break
		}
	}
	result := make([]bool, len(refs))
	for i, a := range refs {
		var err error
		result[i], err = lockRead(ctx, tx, actor, a)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// LockSharedGate precedes all scope-specific grants when sources are combined.
func LockSharedGate(ctx context.Context, tx pgx.Tx) error {
	var scope string
	return tx.QueryRow(ctx, `SELECT scope FROM core.knowledge_scopes WHERE scope='' FOR SHARE`).Scan(&scope)
}

func lockDerivedRevision(ctx context.Context, tx pgx.Tx, actor string, a ReadAuthority) (bool, error) {
	if a.Namespace == "private" && a.Owner != actor {
		return false, nil
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT NOT a.revoked AND r.active AND r.body<>''
 FROM core.memory_read_authorities a JOIN core.memory_revisions r
 USING(namespace,owner,scope,topic,item_key,source_kind,version)
 WHERE a.namespace=$1 AND a.owner=$2 AND a.scope=$3 AND a.topic=$4 AND a.item_key=$5 AND a.source_kind=$6 AND a.version=$7 FOR SHARE OF a,r`, a.Namespace, a.Owner, a.Scope, a.Topic, a.Key, a.SourceKind, a.Version).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return allowed, err
}

func lockDerivedProposal(ctx context.Context, tx pgx.Tx, actor string, a ReadAuthority) (bool, error) {
	if actor != a.Owner {
		err := LockPermission(ctx, tx, actor, a.Scope, Review)
		if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusForbidden {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT NOT a.revoked AND p.body<>'' AND ($4=p.owner OR EXISTS(SELECT 1 FROM core.knowledge_proposal_submissions c WHERE c.proposal_id=p.id AND c.owner=p.owner AND c.scope=p.scope AND c.topic=p.topic AND c.fact_key=p.fact_key AND c.body_sha256=encode(sha256(convert_to(p.body,'UTF8')),'hex') AND c.proposal_version<p.version)) FROM core.knowledge_proposal_authorities a
 JOIN core.knowledge_proposals p ON p.id=a.proposal_id WHERE p.id=$1 AND p.owner=$2 AND p.scope=$3 FOR SHARE OF a,p`, a.ProposalID, a.Owner, a.Scope, actor).
		Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return valid, err
}

func (a ReadAuthority) validDerivedMemory() bool {
	return a.Generation == 0 && a.Version > 0 && validPart(a.Topic, false) && validPart(a.Key, false) &&
		validPart(a.Scope, true) &&
		((a.Namespace == "private" && validPart(a.Owner, false) && a.Scope == "" && (a.SourceKind == "memo" || a.SourceKind == "document")) ||
			(a.Namespace == "shared" && a.Owner == "" && a.SourceKind == "fact"))
}
