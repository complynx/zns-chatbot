package knowledge

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Topic counts inherit only the selected derived revisions; ordinary catalog
// rows do not require causal locks. An oversized source union fails explicitly.
func (s Service) authorizeMemoryTopics(
	ctx context.Context,
	actor string,
	q MemoryQuery,
	overview *MemoryOverview,
) error {
	topics := []MemoryTopic{}
	for _, topic := range overview.Topics {
		entries, err := s.derivedTopicEntries(ctx, actor, q, topic)
		if err != nil {
			return err
		}
		allowed, err := s.authorizeMemoryEntries(ctx, actor, entries)
		if err != nil {
			return err
		}
		topic.Count -= len(entries) - len(allowed)
		for _, entry := range allowed {
			overview.ReadAuthorities, err = readsource.Merge(overview.ReadAuthorities, entry.ReadAuthorities)
			if err != nil {
				return err
			}
		}
		if topic.Count > 0 {
			topics = append(topics, topic)
		}
	}
	overview.Topics = topics
	return nil
}

func (s Service) derivedTopicEntries(
	ctx context.Context,
	actor string,
	q MemoryQuery,
	topic MemoryTopic,
) ([]MemoryEntry, error) {
	rows, err := s.DB.Query(
		ctx,
		memoryCandidates+`SELECT d.namespace,d.event,d.topic,d.item_key,d.version,d.source_kind
 FROM resolved d JOIN core.memory_revisions r ON r.namespace=d.namespace AND r.owner=CASE WHEN d.namespace='private' THEN $1 ELSE '' END
 AND r.scope=d.event AND r.topic=d.topic AND r.item_key=d.item_key AND r.version=d.version AND r.source_kind=d.source_kind
 WHERE r.origin='derived' AND d.namespace=$5 ORDER BY d.event,d.item_key,d.source_kind LIMIT $6`,
		actor,
		q.Event,
		topic.Topic,
		q.Namespace,
		topic.Namespace,
		readsource.MaxAuthorities+1,
	)
	if err != nil {
		return nil, err
	}
	entries := []MemoryEntry{}
	for rows.Next() {
		entry := MemoryEntry{}
		if err = rows.Scan(
			&entry.Namespace,
			&entry.Event,
			&entry.Topic,
			&entry.Key,
			&entry.Version,
			&entry.SourceKind,
		); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(entries) > readsource.MaxAuthorities {
		return nil, readsource.ErrLimit
	}
	return entries, nil
}
