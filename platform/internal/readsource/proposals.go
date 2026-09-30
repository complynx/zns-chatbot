package readsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

// ExpandProposalSources loads bounded flat proposal and memory provenance.
// Loaders reject cycles and forward references using proposal identity order
// or memory revision chronology, without recursively traversing source graphs.
func ExpandProposalSources(ctx context.Context, tx pgx.Tx, refs []Authority) ([]Authority, error) {
	closures, err := proposalClosures(ctx, tx, refs)
	if err != nil {
		return nil, err
	}
	return Merge(closures...)
}

func proposalClosures(ctx context.Context, tx pgx.Tx, refs []Authority) ([][]Authority, error) {
	result := make([][]Authority, len(refs))
	cache := map[knowledgeauthority.ReadAuthority][]Authority{}
	for i, a := range refs {
		if !Valid([]Authority{a}) {
			return nil, ErrLimit
		}
		result[i] = CloneAuthorities([]Authority{a})
		for _, leaf := range sourceLeaves([]Authority{a}) {
			if leaf.Knowledge.Kind != knowledgeauthority.DerivedProposal &&
				leaf.Knowledge.Kind != knowledgeauthority.DerivedMemory {
				continue
			}
			key := leaf.Knowledge
			evidence, ok := cache[key]
			if !ok {
				var err error
				evidence, err = derivedClosureEvidence(ctx, tx, key)
				if err != nil {
					return nil, err
				}
				cache[key] = evidence
			}
			var err error
			result[i], err = Merge(result[i], inheritedMemoryEvidence(a, key, evidence))
			if err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func proposalEvidence(ctx context.Context, tx pgx.Tx, a knowledgeauthority.ReadAuthority) ([]Authority, error) {
	var raw []byte
	var submitted bool
	err := tx.QueryRow(ctx, `SELECT authority.authorities,EXISTS(SELECT 1 FROM core.knowledge_proposal_submissions c
 WHERE c.proposal_id=p.id AND c.owner=p.owner AND c.scope=p.scope AND c.topic=p.topic AND c.fact_key=p.fact_key
 AND c.body_sha256=encode(sha256(convert_to(p.body,'UTF8')),'hex') AND c.proposal_version<p.version) FROM core.knowledge_proposals p
 JOIN core.knowledge_proposal_authorities authority ON authority.proposal_id=p.id
 WHERE p.id=$1 AND p.owner=$2 AND p.scope=$3`, a.ProposalID, a.Owner, a.Scope).Scan(&raw, &submitted)
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
	for i, group := range refs {
		if group.Causal == nil {
			return nil, ErrLimit
		}
		for _, leaf := range group.Causal.Authorities {
			if leaf.Knowledge.Kind == knowledgeauthority.DerivedProposal && leaf.Knowledge.ProposalID >= a.ProposalID {
				return nil, ErrLimit
			}
		}
		if submitted {
			refs[i].Causal.Published = true
		}
	}
	return refs, nil
}

func lockProposalValidity(ctx context.Context, tx pgx.Tx, actor string, refs []Authority) (Validity, error) {
	closures, err := proposalClosures(ctx, tx, refs)
	if err != nil {
		return Validity{}, err
	}
	expanded := proposalWindowAuthorities(closures)
	checked, err := lockFlatValidity(ctx, tx, actor, expanded)
	if err != nil {
		return Validity{}, err
	}
	result := Validity{Origin: make([]bool, len(refs)), Reader: make([]bool, len(refs))}
	for i, closure := range closures {
		result.Origin[i], result.Reader[i] = true, true
		for _, a := range closure {
			index := slices.IndexFunc(expanded, func(other Authority) bool { return Equal(a, other) })
			if index < 0 {
				return Validity{}, ErrLimit
			}
			result.Origin[i] = result.Origin[i] && checked.Origin[index]
			result.Reader[i] = result.Reader[i] && checked.Reader[index]
		}
	}
	return result, nil
}

// Each closure is bounded; a history window may combine many valid records.
// Deduplicate checks without applying the persisted-record budget to the window.
// Keep the same canonical order as Merge before the single global lock pass.
func proposalWindowAuthorities(closures [][]Authority) []Authority {
	result := []Authority{}
	for _, closure := range closures {
		for _, a := range closure {
			if !slices.ContainsFunc(result, func(other Authority) bool { return Equal(a, other) }) {
				result = append(result, CloneAuthorities([]Authority{a})[0])
			}
		}
	}
	slices.SortFunc(result, func(a, b Authority) int {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return bytes.Compare(x, y)
	})
	return result
}

func derivedClosureEvidence(ctx context.Context, tx pgx.Tx, key knowledgeauthority.ReadAuthority) ([]Authority, error) {
	if key.Kind == knowledgeauthority.DerivedMemory {
		return memoryEvidence(ctx, tx, key)
	}
	return proposalEvidence(ctx, tx, key)
}

// Publication changes reader visibility, never the origin's live checks.
// Clone before propagating it: the same cached source may also be read privately.
func inheritedMemoryEvidence(parent Authority, key knowledgeauthority.ReadAuthority, evidence []Authority) []Authority {
	if key.Kind != knowledgeauthority.DerivedMemory || parent.Causal == nil || !parent.Causal.Published {
		return evidence
	}
	published := CloneAuthorities(evidence)
	for _, group := range published {
		group.Causal.Published = true
	}
	return published
}
