package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type proposalCausalRecord struct {
	id               int64
	refs             []readsource.Authority
	derived, revoked bool
	submitted        bool
}

func loadProposalCausal(ctx context.Context, tx pgx.Tx, id int64) (proposalCausalRecord, error) {
	var origin string
	var raw []byte
	result := proposalCausalRecord{id: id}
	err := tx.QueryRow(ctx, `SELECT p.origin,a.authorities,COALESCE(a.revoked,false),EXISTS(SELECT 1 FROM core.knowledge_proposal_submissions c WHERE c.proposal_id=p.id AND c.owner=p.owner AND c.scope=p.scope AND c.topic=p.topic AND c.fact_key=p.fact_key AND c.body_sha256=encode(sha256(convert_to(p.body,'UTF8')),'hex') AND c.proposal_version<p.version) FROM core.knowledge_proposals p
 LEFT JOIN core.knowledge_proposal_authorities a ON a.proposal_id=p.id WHERE p.id=$1`, id).
		Scan(&origin, &raw, &result.revoked, &result.submitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	if origin == "original" && raw == nil {
		return result, nil
	}
	if origin != "derived" || raw == nil {
		return result, errors.New("missing proposal causal authority")
	}
	result.derived = true
	if err = json.Unmarshal(raw, &result.refs); err != nil {
		return result, err
	}
	if result.refs == nil || !readsource.Valid(result.refs) {
		return result, errors.New("invalid proposal causal authority")
	}
	for _, ref := range result.refs {
		if ref.Causal == nil {
			return result, errors.New("invalid proposal causal origin")
		}
	}
	if result.submitted {
		result.refs = publishedProposalRefs(result.refs)
	}
	return result, nil
}

func publishedProposalRefs(refs []readsource.Authority) []readsource.Authority {
	result := readsource.CloneAuthorities(refs)
	for i := range result {
		if result[i].Causal != nil {
			result[i].Causal.Published = true
		}
	}
	return result
}

func bindProposalCausal(ctx context.Context, tx pgx.Tx, p *Proposal, refs []readsource.Authority) error {
	for _, group := range refs {
		if group.Causal == nil {
			return errors.New("invalid proposal source")
		}
		for _, leaf := range group.Causal.Authorities {
			if leaf.Knowledge.Kind == knowledgeauthority.DerivedProposal && leaf.Knowledge.ProposalID >= p.ID {
				return errors.New("invalid proposal ancestry")
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE core.knowledge_proposals SET origin='derived' WHERE id=$1`, p.ID); err != nil {
		return core.DatabaseOperationError(err)
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.knowledge_proposal_authorities(proposal_id,authorities) VALUES($1,$2)`,
		p.ID,
		refs,
	); err != nil {
		return core.DatabaseOperationError(err)
	}
	var err error
	p.ReadAuthorities, err = proposalReadAuthorities(*p)
	return err
}

func proposalReadAuthorities(p Proposal) ([]readsource.Authority, error) {
	return readsource.Merge(
		[]readsource.Authority{
			{
				Knowledge: knowledgeauthority.ReadAuthority{
					Kind:       knowledgeauthority.DerivedProposal,
					ProposalID: p.ID,
					Owner:      p.Owner,
					Scope:      p.Event,
				},
			},
		},
	)
}

func (s Service) authorizeProposals(ctx context.Context, actor string, proposals []Proposal) ([]Proposal, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	records := make([]proposalCausalRecord, len(proposals))
	refs := []readsource.Authority{}
	for i, p := range proposals {
		records[i], err = loadProposalCausal(ctx, tx, p.ID)
		if err != nil {
			return nil, err
		}
		refs, err = readsource.Merge(refs, records[i].refs)
		if err != nil {
			return nil, err
		}
	}
	if err = readsource.LockEvents(ctx, tx, refs); err != nil {
		return nil, err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor}, refs); err != nil {
		return nil, err
	}
	validity, err := readsource.LockValidity(ctx, tx, actor, refs)
	if err != nil {
		return nil, err
	}
	result := []Proposal{}
	for i, p := range proposals {
		var allowed bool
		p, allowed, err = exposeProposal(ctx, tx, actor, p, records[i], refs, validity)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		result = append(result, p)
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

func lockProposalCausal(ctx context.Context, tx pgx.Tx, actor string, record proposalCausalRecord) error {
	if !record.derived {
		return nil
	}
	allowed, err := readsource.Lock(ctx, tx, actor, record.refs)
	if err != nil {
		return err
	}
	var revoked bool
	if err = tx.QueryRow(ctx, `SELECT revoked FROM core.knowledge_proposal_authorities WHERE proposal_id=$1 FOR SHARE`, record.id).
		Scan(&revoked); err != nil {
		return core.DatabaseOperationError(err)
	}
	if revoked || slices.Contains(allowed, false) {
		return conflict("knowledge_stale")
	}
	return nil
}

// A manual approval publishes this exact immutable proposal body. It keeps
// origin invalidation evidence without delegating the publisher's source grants.
func publishProposalCausal(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	result Result,
	record proposalCausalRecord,
) error {
	if !record.derived || result.Fact == nil {
		return nil
	}
	refs := readsource.CloneAuthorities(record.refs)
	p := result.Proposal
	leaf := readsource.Authority{
		Knowledge: knowledgeauthority.ReadAuthority{
			Kind:       knowledgeauthority.DerivedProposal,
			ProposalID: p.ID,
			Owner:      p.Owner,
			Scope:      p.Event,
		},
	}
	attached := false
	for i := range refs {
		if refs[i].Causal.Actor == p.Owner && !attached {
			var err error
			refs[i].Causal.Authorities, err = readsource.Merge(refs[i].Causal.Authorities, []readsource.Authority{leaf})
			if err != nil {
				return err
			}
			attached = true
		}
		refs[i].Causal.Published = true
	}
	if !attached || !readsource.Valid(refs) {
		return readsource.ErrLimit
	}
	return bindMemoryCausalRefs(ctx, tx, actor, result, refs)
}

func authorizeProposalRecord(
	ctx context.Context,
	tx pgx.Tx,
	id int64,
	record proposalCausalRecord,
	refs []readsource.Authority,
	validity readsource.Validity,
) (bool, error) {
	var revoked bool
	if err := tx.QueryRow(ctx, `SELECT revoked FROM core.knowledge_proposal_authorities WHERE proposal_id=$1 FOR UPDATE`, id).
		Scan(&revoked); err != nil {
		return false, core.DatabaseOperationError(err)
	}
	origin, reader := memoryCausalValidity(record.refs, refs, validity)
	if !origin {
		if _, err := tx.Exec(
			ctx,
			`UPDATE core.knowledge_proposal_authorities SET revoked=true WHERE proposal_id=$1`,
			id,
		); err != nil {
			return false, core.DatabaseOperationError(err)
		}
	}
	if revoked || !origin || !reader {
		return false, nil
	}
	return true, nil
}

func exposeProposal(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	p Proposal,
	record proposalCausalRecord,
	refs []readsource.Authority,
	validity readsource.Validity,
) (Proposal, bool, error) {
	if actor != p.Owner {
		if !p.Submitted {
			return Proposal{}, false, nil
		}
		if err := knowledgeauthority.LockPermission(ctx, tx, actor, p.Event, Review); err != nil {
			return Proposal{}, false, err
		}
		p.Reason = ""
	}
	if !record.derived {
		return p, true, nil
	}
	allowed, err := authorizeProposalRecord(ctx, tx, p.ID, record, refs, validity)
	if err != nil || !allowed {
		return Proposal{}, false, err
	}
	p.ReadAuthorities, err = proposalReadAuthorities(p)
	return p, err == nil, err
}
