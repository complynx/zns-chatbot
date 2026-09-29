package knowledge

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func factVersion(ctx context.Context, tx pgx.Tx, c Command) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, `SELECT version FROM core.knowledge_facts WHERE scope=$1 AND topic=$2 AND fact_key=$3`, c.Event, c.Topic, c.FactKey).
		Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func curate(ctx context.Context, tx pgx.Tx, c Command) (Result, error) {
	version, err := factVersion(ctx, tx, c)
	if err != nil {
		return Result{}, err
	}
	if version != c.Version {
		return Result{}, conflict("knowledge_stale")
	}
	if c.Name == RemoveFact && version == 0 {
		return Result{}, missing()
	}
	fact := Fact{
		Event:     c.Event,
		Topic:     c.Topic,
		Key:       c.FactKey,
		Text:      c.Text,
		Version:   version + 1,
		Active:    c.Name != RemoveFact,
		Untrusted: true,
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.knowledge_facts(scope,topic,fact_key,body,version,active) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(scope,topic,fact_key) DO UPDATE SET body=excluded.body,version=excluded.version,active=excluded.active,updated_at=clock_timestamp()`,
		c.Event,
		c.Topic,
		c.FactKey,
		c.Text,
		fact.Version,
		fact.Active,
	)
	if err != nil {
		return Result{}, err
	}
	if c.Event == "" {
		fact.Phase = "general"
	} else {
		err = tx.QueryRow(ctx, `SELECT CASE WHEN finishes_at<=clock_timestamp() THEN 'past' ELSE 'active_or_upcoming' END FROM core.events WHERE id=$1`, c.Event).
			Scan(&fact.Phase)
	}
	return Result{Fact: &fact}, err
}

func suggest(ctx context.Context, tx pgx.Tx, actor string, c Command) (Result, error) {
	if c.Version != 0 {
		return Result{}, invalid()
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM core.knowledge_proposals WHERE owner=$1 AND state IN ('pending_filter','awaiting_submission','pending_review')`, actor).
		Scan(&count)
	if err != nil {
		return Result{}, err
	}
	if count >= MaxPending {
		return Result{}, conflict("knowledge_pending_capacity")
	}
	version, err := factVersion(ctx, tx, c)
	if err != nil {
		return Result{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO core.knowledge_proposals(scope,owner,topic,fact_key,body,fact_version,state) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		c.Event, actor, c.Topic, c.FactKey, c.Text, version, pendingFilter).
		Scan(&id)
	if err != nil {
		return Result{}, err
	}
	p, err := readProposal(ctx, tx, id, c.Event)
	return Result{Proposal: &p}, err
}

const proposalColumns = `id,scope,owner,topic,fact_key,body,version,fact_version,state,reason,created_at,` + proposalSubmittedSQL

func readProposal(ctx context.Context, tx pgx.Tx, id int64, scope string) (Proposal, error) {
	p, err := scanProposal(
		tx.QueryRow(
			ctx,
			`SELECT `+proposalColumns+` FROM core.knowledge_proposals WHERE id=$1 AND scope=$2 FOR UPDATE`,
			id,
			scope,
		),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, missing()
	}
	return p, err
}

func scanProposal(row pgx.Row) (Proposal, error) {
	var p Proposal
	err := row.Scan(
		&p.ID,
		&p.Event,
		&p.Owner,
		&p.Topic,
		&p.FactKey,
		&p.Text,
		&p.Version,
		&p.FactVersion,
		&p.State,
		&p.Reason,
		&p.CreatedAt,
		&p.Submitted,
	)
	return p, err
}

func decide(ctx context.Context, tx pgx.Tx, actor string, c Command) (Result, error) {
	p, err := readProposal(ctx, tx, c.ProposalID, c.Event)
	if err != nil {
		return Result{}, err
	}
	if (c.Name == assess && p.Owner != actor) || (c.Name == Review && p.Owner == actor) {
		return Result{}, forbidden()
	}
	if p.Version != c.Version {
		return Result{}, conflict("knowledge_stale")
	}
	if c.Name == assess {
		return assessProposal(ctx, tx, p, c)
	}
	if p.State != pendingReview || !p.Submitted {
		return Result{}, conflict("knowledge_review_state")
	}
	p.State = "rejected"
	result := Result{}
	if c.Decision == approve {
		result, err = curate(
			ctx,
			tx,
			Command{
				Name:    Curate,
				Event:   p.Event,
				Topic:   p.Topic,
				FactKey: p.FactKey,
				Text:    p.Text,
				Version: p.FactVersion,
			},
		)
		if err != nil {
			return Result{}, err
		}
		p.State = "approved"
	}
	p, err = updateProposal(ctx, tx, p, c.Text)
	result.Proposal = &p
	return result, err
}

func assessProposal(ctx context.Context, tx pgx.Tx, p Proposal, c Command) (Result, error) {
	if p.State != pendingFilter {
		return Result{}, conflict("knowledge_filter_state")
	}
	p.State = "filtered"
	if c.Decision == approve {
		p.State = AwaitingSubmission
	}
	p, err := updateProposal(ctx, tx, p, c.Text)
	return Result{Proposal: &p}, err
}

func updateProposal(ctx context.Context, tx pgx.Tx, p Proposal, reason string) (Proposal, error) {
	p.Version++
	p.Reason = reason
	_, err := tx.Exec(
		ctx,
		`UPDATE core.knowledge_proposals SET state=$2,version=$3,reason=$4,updated_at=clock_timestamp() WHERE id=$1`,
		p.ID,
		p.State,
		p.Version,
		p.Reason,
	)
	return p, err
}

func writeMemo(ctx context.Context, tx pgx.Tx, actor string, c Command) (Result, error) {
	var version int64
	err := tx.QueryRow(ctx, `SELECT version FROM core.knowledge_memos WHERE owner=$1 AND memo_key=$2`, actor, c.FactKey).
		Scan(&version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if version != c.Version {
		return Result{}, conflict("knowledge_stale")
	}
	if c.Name == MemoDelete && version == 0 {
		return Result{}, missing()
	}
	if c.Name == MemoSet {
		var count int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM core.knowledge_memos WHERE owner=$1 AND active AND memo_key<>$2`, actor, c.FactKey).
			Scan(&count)
		if err != nil {
			return Result{}, err
		}
		if count >= MaxMemos {
			return Result{}, conflict("knowledge_memo_capacity")
		}
	}
	memo := Memo{Key: c.FactKey, Text: c.Text, Version: version + 1, Active: c.Name == MemoSet}
	_, err = tx.Exec(ctx, `INSERT INTO core.knowledge_memos(owner,memo_key,body,version,active) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(owner,memo_key) DO UPDATE SET body=excluded.body,version=excluded.version,active=excluded.active`, actor, c.FactKey, c.Text, memo.Version, memo.Active)
	return Result{Memo: &memo}, err
}
