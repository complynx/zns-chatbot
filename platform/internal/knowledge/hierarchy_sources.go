package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

// ExecuteWithSources is host-only. Source keys come from archived host requests,
// never model arguments. Ordinary HTTP commands cannot supply these keys.
func (s Service) ExecuteWithSources(ctx context.Context, actor string, c Command, keys []string) (Result, error) {
	keys = slices.Clone(keys)
	slices.Sort(keys)
	keys = slices.Compact(keys)
	return s.execute(ctx, actor, c, false, keys)
}

func memoryCommandBytes(c Command, keys []string) ([]byte, error) {
	if len(keys) == 0 {
		return json.Marshal(c)
	}
	return json.Marshal(struct {
		Command    Command  `json:"command"`
		SourceKeys []string `json:"source_keys"`
	}{c, keys})
}

func resolveMemorySources(ctx context.Context, tx pgx.Tx, actor string, keys []string) ([]int64, error) {
	const maxSources = 8
	const maxSourceKey = 200
	if len(keys) > maxSources {
		return nil, invalid()
	}
	ids := make([]int64, 0, len(keys))
	for _, key := range keys {
		if !validText(key, maxSourceKey, false) {
			return nil, invalid()
		}
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner=$1 AND source_key=$2 FOR SHARE`, actor, key).
			Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, missing()
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func resultMemoryReference(result Result) (memoryReference, bool) {
	switch {
	case result.Document != nil:
		d := result.Document
		return memoryReference{
			Namespace:  MemoryPrivate,
			Topic:      d.Topic,
			Key:        d.Key,
			Version:    d.Version,
			SourceKind: MemoryDocumentKind,
		}, d.Active
	case result.Memo != nil:
		m := result.Memo
		return memoryReference{
			Namespace:  MemoryPrivate,
			Topic:      memoryPrivateTopic(m.Key),
			Key:        m.Key,
			Version:    m.Version,
			SourceKind: MemoryMemoKind,
		}, m.Active
	case result.Fact != nil:
		f := result.Fact
		return memoryReference{
			Namespace:      MemoryShared,
			Event:          f.Event,
			Topic:          f.Topic,
			Key:            f.Key,
			Version:        f.Version,
			SourceKind:     MemoryFactKind,
			RequestedEvent: f.Event,
		}, f.Active
	default:
		return memoryReference{}, false
	}
}

func bindMemorySources(ctx context.Context, tx pgx.Tx, actor string, result Result, ids []int64) error {
	if result.Proposal != nil && result.Fact == nil {
		for _, id := range ids {
			_, err := tx.Exec(
				ctx,
				`INSERT INTO core.memory_proposal_sources(proposal_id,source_owner,event_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
				result.Proposal.ID,
				actor,
				id,
			)
			if err != nil {
				return err
			}
		}
		return nil
	}
	ref, active := resultMemoryReference(result)
	if !active {
		return nil
	}
	owner := ""
	if ref.Namespace == MemoryPrivate {
		owner = actor
	}
	for _, id := range ids {
		_, err := tx.Exec(
			ctx,
			`INSERT INTO core.memory_sources(namespace,owner,scope,topic,item_key,source_kind,version,source_owner,event_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`,
			ref.Namespace,
			owner,
			ref.Event,
			ref.Topic,
			ref.Key,
			ref.SourceKind,
			ref.Version,
			actor,
			id,
		)
		if err != nil {
			return err
		}
	}
	if result.Proposal != nil {
		_, err := tx.Exec(
			ctx,
			`INSERT INTO core.memory_sources(namespace,owner,scope,topic,item_key,source_kind,version,source_owner,event_id)
 SELECT $1,$2,$3,$4,$5,$6,$7,source_owner,event_id FROM core.memory_proposal_sources WHERE proposal_id=$8 ON CONFLICT DO NOTHING`,
			ref.Namespace,
			owner,
			ref.Event,
			ref.Topic,
			ref.Key,
			ref.SourceKind,
			ref.Version,
			result.Proposal.ID,
		)
		return err
	}
	return nil
}

// MemorySources returns only the reader's own archived source evidence. A
// shared fact never makes its author's private conversation publicly readable.
func (s Service) MemorySources(ctx context.Context, actor, reference string) (conversation.Page, error) {
	ref, err := decodeMemoryReference(reference)
	if err != nil {
		return conversation.Page{}, err
	}
	if err = s.knownActor(ctx, actor); err != nil {
		return conversation.Page{}, err
	}
	if ref.SourceKind == MemorySourceKind {
		if err = s.sourceReferenceCurrent(ctx, ref); err != nil {
			return conversation.Page{}, err
		}
	}
	owner := ""
	if ref.Namespace == MemoryPrivate {
		owner = actor
	}
	rows, err := s.DB.Query(ctx, `SELECT e.id,e.kind,e.text,'{}'::jsonb,e.omitted,e.created_at
 FROM core.memory_sources s JOIN core.conversation_events e ON e.id=s.event_id AND e.owner=s.source_owner
 WHERE s.namespace=$1 AND s.owner=$2 AND s.scope=$3 AND s.topic=$4 AND s.item_key=$5 AND s.source_kind=$6 AND s.version=$7 AND s.source_owner=$8
 ORDER BY e.id LIMIT $9`, ref.Namespace, owner, ref.Event, ref.Topic, ref.Key, ref.SourceKind, ref.Version, actor, conversation.MaxPage)
	if err != nil {
		return conversation.Page{}, err
	}
	defer rows.Close()
	result := conversation.Page{Events: []conversation.Event{}}
	for rows.Next() {
		var event conversation.Event
		if err = rows.Scan(&event.ID, &event.Kind, &event.Text, &event.Details, &event.Omitted, &event.At); err != nil {
			return result, err
		}
		excerpt := memoryExcerpt(event.Text)
		event.Omitted = event.Omitted || excerpt != event.Text
		event.Text = excerpt
		result.Events = append(result.Events, event)
	}
	return result, rows.Err()
}

// AttachMemorySources completes host provenance after privacy-aware archival.
// The actor-scoped operation receipt determines the target, never model fields.
func (s Service) AttachMemorySources(ctx context.Context, actor, operationKey string, keys []string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockActor(ctx, tx, actor); err != nil {
		return err
	}
	var data []byte
	err = tx.QueryRow(ctx, `SELECT result FROM core.knowledge_operations WHERE actor=$1 AND key_hash=$2`, actor, digest([]byte(operationKey))).
		Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var result Result
	if err = json.Unmarshal(data, &result); err != nil {
		return err
	}
	if result.Proposal != nil && result.Fact == nil {
		if err = attachProposalSources(ctx, tx, actor, result, keys); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	ref, active := resultMemoryReference(result)
	if !active {
		return nil
	}
	owner := actor
	if ref.Namespace == MemoryShared {
		owner = ""
		if err = lockScope(ctx, tx, ref.Event); err != nil {
			return err
		}
	}
	var retained bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.memory_revisions WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND source_kind=$6 AND version=$7 AND active AND body<>'')`, ref.Namespace, owner, ref.Event, ref.Topic, ref.Key, ref.SourceKind, ref.Version).
		Scan(&retained)
	if err != nil {
		return err
	}
	if !retained {
		return nil
	}
	ids, err := resolveMemorySources(ctx, tx, actor, keys)
	if err != nil {
		return err
	}
	if err = bindMemorySources(ctx, tx, actor, result, ids); err != nil {
		return err
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM core.memory_sources WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND source_kind=$6 AND version=$7`, ref.Namespace, owner, ref.Event, ref.Topic, ref.Key, ref.SourceKind, ref.Version).
		Scan(&count)
	if err != nil {
		return err
	}
	if count > conversation.MaxPage {
		return invalid()
	}
	return tx.Commit(ctx)
}

func attachProposalSources(ctx context.Context, tx pgx.Tx, actor string, result Result, keys []string) error {
	if result.Proposal.Owner != actor {
		return forbidden()
	}
	if err := lockScope(ctx, tx, result.Proposal.Event); err != nil {
		return err
	}
	ids, err := resolveMemorySources(ctx, tx, actor, keys)
	if err != nil {
		return err
	}
	if err = bindMemorySources(ctx, tx, actor, result, ids); err != nil {
		return err
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM core.memory_proposal_sources WHERE proposal_id=$1`, result.Proposal.ID).
		Scan(&count)
	if err != nil {
		return err
	}
	const maxProposalSources = 8
	if count > maxProposalSources {
		return invalid()
	}
	// Approval can precede deferred archival. Link only the still-retained
	// published revision; deletion must not resurrect its source evidence.
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.memory_sources(namespace,owner,scope,topic,item_key,source_kind,version,source_owner,event_id)
 SELECT 'shared','',p.scope,p.topic,p.fact_key,'fact',r.version,s.source_owner,s.event_id
 FROM core.knowledge_proposals p JOIN core.memory_proposal_sources s ON s.proposal_id=p.id
 JOIN core.memory_revisions r ON r.namespace='shared' AND r.owner='' AND r.scope=p.scope AND r.topic=p.topic
 AND r.item_key=p.fact_key AND r.source_kind='fact' AND r.version=p.fact_version+1 AND r.active AND r.body<>''
 WHERE p.id=$1 AND p.state='approved' ON CONFLICT DO NOTHING`,
		result.Proposal.ID,
	)
	return err
}
