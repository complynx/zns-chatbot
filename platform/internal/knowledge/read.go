package knowledge

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s Service) knownActor(ctx context.Context, actor string) error {
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return forbidden()
	}
	return nil
}

// Retrieve labels the actual source event and its phase. Historical fallback is
// never relabelled as a fact about the requested event. Search follows priority
// selection so an old matching phrase cannot bypass a newer overriding fact.
func (s Service) retrieve(ctx context.Context, actor string, q Query) ([]Fact, error) {
	const maxSearch = 100
	if !validID(q.Event, true) || !validID(q.Topic, true) || !validText(q.Text, maxSearch, true) {
		return nil, invalid()
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return nil, err
	}
	afterTopic, afterKey, cursorErr := decodeFactCursor(q.Cursor)
	if cursorErr != nil {
		return nil, cursorErr
	}
	if q.Event != "" {
		var exists bool
		if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.events WHERE id=$1)`, q.Event).
			Scan(&exists); err != nil {
			return nil, err
		} else if !exists {
			return nil, missing()
		}
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q.Text) + "%"
	rows, err := s.DB.Query(ctx, `WITH moment AS (SELECT statement_timestamp() AS now), target AS (
 SELECT finishes_at FROM core.events WHERE id=$1
), candidates AS (
 SELECT f.scope,f.topic,f.fact_key,f.body,f.version,e.finishes_at,
 CASE WHEN f.scope='' THEN 'general' WHEN e.finishes_at<=moment.now THEN 'past' ELSE 'active_or_upcoming' END AS phase,
 row_number() OVER(PARTITION BY f.topic,f.fact_key ORDER BY
 CASE WHEN f.scope=$1 THEN 0 WHEN f.scope<>'' THEN 1 ELSE 2 END,
 e.finishes_at DESC NULLS LAST,f.scope) AS rank
 FROM core.knowledge_facts f LEFT JOIN core.events e ON e.id=f.scope CROSS JOIN moment
 WHERE f.active AND ($2='' OR f.topic=$2) AND
 (f.scope='' OR f.scope=$1 OR ($1<>'' AND e.finishes_at<=moment.now AND e.finishes_at<=(SELECT finishes_at FROM target)))
)
 SELECT scope,topic,fact_key,body,version,phase,scope<>'' AND scope<>$1
 FROM candidates WHERE rank=1 AND body ILIKE $3 ESCAPE '\' AND (topic,fact_key)>($5,$6)
 ORDER BY topic,fact_key LIMIT $4`, q.Event, q.Topic, pattern, MaxResults+1, afterTopic, afterKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Fact, 0)
	for rows.Next() {
		fact := Fact{Active: true, Untrusted: true}
		if err = rows.Scan(
			&fact.Event,
			&fact.Topic,
			&fact.Key,
			&fact.Text,
			&fact.Version,
			&fact.Phase,
			&fact.HistoricalFallback,
		); err != nil {
			return nil, err
		}
		result = append(result, fact)
	}
	return result, rows.Err()
}

func (s Service) Proposals(ctx context.Context, actor string, q ProposalQuery) ([]Proposal, error) {
	if !validID(q.Event, true) || q.After < 0 {
		return nil, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockActor(ctx, tx, actor); err != nil {
		return nil, err
	}
	if q.ReviewQueue {
		if err = authorize(ctx, tx, actor, Command{Name: Review, Event: q.Event}); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT `+proposalColumns+` FROM core.knowledge_proposals WHERE scope=$1
 AND (($2 AND state='pending_review' AND owner<>$3) OR (NOT $2 AND owner=$3))
 AND ($4::bigint=0 OR id<$4) ORDER BY id DESC LIMIT $5`, q.Event, q.ReviewQueue, actor, q.After, MaxResults)
	if err != nil {
		return nil, err
	}
	result := make([]Proposal, 0)
	for rows.Next() {
		p, scanErr := scanProposal(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		result = append(result, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func (s Service) Memos(ctx context.Context, actor string) ([]Memo, error) {
	if err := s.knownActor(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT memo_key,body,version,active FROM core.knowledge_memos WHERE owner=$1 AND active ORDER BY memo_key LIMIT $2`,
		actor,
		MaxMemos,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Memo, 0)
	for rows.Next() {
		var memo Memo
		if err = rows.Scan(&memo.Key, &memo.Text, &memo.Version, &memo.Active); err != nil {
			return nil, err
		}
		result = append(result, memo)
	}
	return result, rows.Err()
}

func (s Service) Memo(ctx context.Context, actor, key string) (Memo, error) {
	if !validID(key, false) {
		return Memo{}, invalid()
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return Memo{}, err
	}
	memo := Memo{Key: key}
	err := s.DB.QueryRow(ctx, `SELECT body,version,active FROM core.knowledge_memos WHERE owner=$1 AND memo_key=$2`, actor, key).
		Scan(&memo.Text, &memo.Version, &memo.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return memo, nil
	}
	return memo, err
}
