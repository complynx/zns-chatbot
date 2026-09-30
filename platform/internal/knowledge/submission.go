package knowledge

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const AwaitingSubmission = "awaiting_submission"

// Submission is the exact author-visible proposal bound to a manual action.
// It is not a conversational Command and cannot be submitted by an agent tool.
type Submission struct {
	ProposalID int64  `json:"proposal_id"`
	Version    int64  `json:"version"`
	Event      string `json:"event"`
	Topic      string `json:"topic"`
	FactKey    string `json:"fact_key"`
	Text       string `json:"text"`
}

const proposalSubmittedSQL = `EXISTS(SELECT 1 FROM core.knowledge_proposal_submissions consent
 WHERE consent.proposal_id=core.knowledge_proposals.id AND consent.owner=core.knowledge_proposals.owner
 AND consent.scope=core.knowledge_proposals.scope AND consent.topic=core.knowledge_proposals.topic
 AND consent.fact_key=core.knowledge_proposals.fact_key AND consent.body_sha256=encode(sha256(convert_to(core.knowledge_proposals.body,'UTF8')),'hex')
 AND consent.proposal_version<core.knowledge_proposals.version)`

// SubmitProposal is called only by the authenticated manual host boundary.
// Consent publishes this body to destination reviewers, never its private inputs.
func (s Service) SubmitProposal(ctx context.Context, actor string, input Submission) (Result, error) {
	if input.ProposalID <= 0 || input.Version <= 0 || !validID(input.Event, true) ||
		!validID(input.Topic, false) || !validID(input.FactKey, false) || !validText(input.Text, MaxText, false) {
		return Result{}, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Result{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := loadProposalCausal(ctx, tx, input.ProposalID)
	if err != nil {
		return Result{}, err
	}
	if err = lockKnowledgeCommand(
		ctx,
		tx,
		actor,
		Command{Event: input.Event},
		record.refs,
		record.derived,
	); err != nil {
		return Result{}, err
	}
	p, err := readProposal(ctx, tx, input.ProposalID, input.Event)
	if err != nil {
		return Result{}, err
	}
	if p.Owner != actor {
		return Result{}, forbidden()
	}
	if p.Topic != input.Topic || p.FactKey != input.FactKey || p.Text != input.Text {
		return Result{}, conflict("knowledge_stale")
	}
	if err = lockProposalCausal(ctx, tx, actor, record); err != nil {
		return Result{}, err
	}
	p, err = recordProposalConsent(ctx, tx, p, input)
	if err != nil {
		return Result{}, err
	}
	if record.derived {
		p.ReadAuthorities, err = proposalReadAuthorities(p)
		if err != nil {
			return Result{}, err
		}
	}
	return Result{Proposal: &p}, core.DatabaseOperationError(tx.Commit(ctx))
}

func recordProposalConsent(ctx context.Context, tx pgx.Tx, p Proposal, input Submission) (Proposal, error) {
	var previous int64
	err := tx.QueryRow(ctx, `SELECT proposal_version FROM core.knowledge_proposal_submissions WHERE proposal_id=$1`, p.ID).
		Scan(&previous)
	if err == nil {
		if !p.Submitted || previous != input.Version {
			return Proposal{}, conflict("knowledge_stale")
		}
		return p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, core.DatabaseOperationError(err)
	}
	if p.Version != input.Version || p.State != AwaitingSubmission {
		return Proposal{}, conflict("knowledge_submission_state")
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.knowledge_proposal_submissions(proposal_id,owner,proposal_version,scope,topic,fact_key,body_sha256)
 VALUES($1,$2,$3,$4,$5,$6,$7)`,
		p.ID,
		p.Owner,
		p.Version,
		p.Event,
		p.Topic,
		p.FactKey,
		digest([]byte(p.Text)),
	)
	if err != nil {
		return Proposal{}, core.DatabaseOperationError(err)
	}
	p.State = pendingReview
	p, err = updateProposal(ctx, tx, p, p.Reason)
	if err != nil {
		return Proposal{}, err
	}
	p.Submitted = true
	return p, nil
}
