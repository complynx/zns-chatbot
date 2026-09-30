package knowledge

import (
	"context"

	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// MemorySummary is a small navigation surface. Authors write summary records
// through the same moderated fact commands or owner-private memo commands.
func (s Service) MemorySummary(ctx context.Context, actor string, query MemoryQuery) (MemoryOverview, error) {
	q, err := normalizeMemoryQuery(query)
	if err != nil {
		return MemoryOverview{}, err
	}
	if err = s.validateMemoryActor(ctx, actor, q); err != nil {
		return MemoryOverview{}, err
	}
	result := MemoryOverview{Summaries: []MemoryEntry{}, Topics: []MemoryTopic{}, Untrusted: true}
	rows, err := s.DB.Query(
		ctx,
		memoryCandidates+`SELECT namespace,topic,count(*) FROM resolved GROUP BY namespace,topic ORDER BY namespace,topic LIMIT $5`,
		actor,
		q.Event,
		"",
		q.Namespace,
		MaxResults+1,
	)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	for rows.Next() {
		var topic MemoryTopic
		if err = rows.Scan(&topic.Namespace, &topic.Topic, &topic.Count); err != nil {
			rows.Close()
			return result, core.DatabaseOperationError(err)
		}
		result.Topics = append(result.Topics, topic)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	if len(result.Topics) > MaxResults {
		result.More = true
		result.Topics = result.Topics[:MaxResults]
	}
	if err = s.authorizeMemoryTopics(ctx, actor, q, &result); err != nil {
		return MemoryOverview{}, err
	}
	summaries, err := s.SearchMemory(ctx, actor, MemoryQuery{Namespace: q.Namespace, Event: q.Event, Topic: "summary"})
	result.Summaries = summaries.Entries
	result.More = result.More || summaries.More
	for len(result.Summaries) > 0 && !memoryFits(result) {
		result.Summaries = result.Summaries[:len(result.Summaries)-1]
		result.More = true
	}
	return result, err
}

func decodeMemoryReference(value string) (memoryReference, error) {
	var ref memoryReference
	if memoryDecode(value, &ref) != nil {
		return ref, invalid()
	}
	if ref.SourceKind == "" {
		ref.SourceKind = MemoryFactKind
		if ref.Namespace == MemoryPrivate {
			ref.SourceKind = MemoryMemoKind
		}
	}
	if (ref.Namespace != MemoryShared && ref.Namespace != MemoryPrivate) ||
		!validID(
			ref.RequestedEvent,
			true,
		) || !validID(ref.Event, true) || !validID(ref.Topic, false) || !validID(ref.Key, false) || ref.Version <= 0 ||
		(ref.Namespace == MemoryPrivate && (ref.Event != "" || (ref.SourceKind == MemoryMemoKind && ref.Topic != memoryPrivateTopic(ref.Key)))) ||
		(ref.Namespace == MemoryShared && ref.SourceKind != MemoryFactKind && ref.SourceKind != MemorySourceKind) ||
		(ref.Namespace == MemoryPrivate && ref.SourceKind != MemoryMemoKind && ref.SourceKind != MemoryDocumentKind) {
		return ref, invalid()
	}
	return ref, nil
}

// ReadMemory requires the indexed version. Refresh the index after a stale
// reference; current reads never silently return an older revision.
func (s Service) ReadMemory(ctx context.Context, actor, reference string) (MemoryEntry, error) {
	return s.ReadMemoryPage(ctx, actor, reference, "")
}

func (s Service) readMemoryCurrent(ctx context.Context, actor, reference string) (MemoryEntry, error) {
	ref, err := decodeMemoryReference(reference)
	if err != nil {
		return MemoryEntry{}, err
	}
	entry := MemoryEntry{
		Ref:        reference,
		Namespace:  ref.Namespace,
		Event:      ref.Event,
		Topic:      ref.Topic,
		Key:        ref.Key,
		Version:    ref.Version,
		Untrusted:  true,
		SourceKind: ref.SourceKind,
	}
	if ref.SourceKind == MemoryDocumentKind {
		return s.readDocument(ctx, actor, ref, entry)
	}
	if ref.SourceKind == MemorySourceKind {
		if ref.Event != "" || !sourceSlot(ref.Topic) {
			return MemoryEntry{}, invalid()
		}
		return s.readSource(ctx, actor, ref, entry)
	}
	if ref.Namespace == MemoryPrivate {
		return s.readPrivateMemory(ctx, actor, ref, entry)
	}
	fact, err := s.Fact(ctx, actor, ref.Event, ref.Topic, ref.Key)
	if err != nil {
		return entry, err
	}
	if !fact.Active || fact.Version != ref.Version {
		return MemoryEntry{}, conflict("knowledge_stale")
	}
	entry.Text, entry.Active, entry.Phase = fact.Text, fact.Active, fact.Phase
	entry.HistoricalFallback = ref.Event != "" && ref.Event != ref.RequestedEvent
	return entry, nil
}

// MemoryHistory exposes captured revisions, not an invented conversation log.
// Owner filtering precedes reading private history. Deleted bodies are scrubbed.
func (s Service) MemoryHistory(ctx context.Context, actor, reference, cursor string) (MemoryPage, error) {
	ref, err := decodeMemoryReference(reference)
	if err != nil {
		return MemoryPage{}, err
	}
	if err = s.knownActor(ctx, actor); err != nil {
		return MemoryPage{}, err
	}
	if ref.SourceKind == MemorySourceKind {
		if err = s.sourceReferenceCurrent(ctx, ref); err != nil {
			return MemoryPage{}, err
		}
	}
	after, err := memoryHistoryPosition(actor, reference, cursor)
	if err != nil {
		return MemoryPage{}, err
	}
	owner := ""
	if ref.Namespace == MemoryPrivate {
		owner = actor
	}
	rows, err := s.DB.Query(ctx, `SELECT version,body,active,captured_at FROM core.memory_revisions
 WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND version>$6 AND source_kind=$8
 AND ($8<>'source' OR EXISTS(SELECT 1 FROM core.assistant_source_documents d JOIN core.assistant_sources s ON s.slot=d.slot AND s.identity=d.identity AND s.digest<>'' WHERE d.slot=$4 AND d.item_key=$5))
 ORDER BY version LIMIT $7`, ref.Namespace, owner, ref.Event, ref.Topic, ref.Key, after, MaxResults+1, ref.SourceKind)
	if err != nil {
		return MemoryPage{}, core.DatabaseOperationError(err)
	}
	defer rows.Close()
	page := MemoryPage{Entries: []MemoryEntry{}}
	for rows.Next() {
		entry := MemoryEntry{
			Namespace:  ref.Namespace,
			Event:      ref.Event,
			Topic:      ref.Topic,
			Key:        ref.Key,
			Untrusted:  true,
			Historical: true,
			Phase:      "revision",
			SourceKind: ref.SourceKind,
		}
		if err = rows.Scan(&entry.Version, &entry.Text, &entry.Active, &entry.CapturedAt); err != nil {
			return MemoryPage{}, core.DatabaseOperationError(err)
		}
		entry.Ref = memoryEncode(
			memoryReference{
				Namespace:      ref.Namespace,
				Event:          ref.Event,
				Topic:          ref.Topic,
				Key:            ref.Key,
				Version:        entry.Version,
				RequestedEvent: ref.RequestedEvent,
				SourceKind:     ref.SourceKind,
			},
		)
		entry.Text = memoryExcerpt(entry.Text)
		page.Entries = append(page.Entries, entry)
	}
	if err = rows.Err(); err != nil {
		return page, core.DatabaseOperationError(err)
	}
	if len(page.Entries) > MaxResults {
		page.More = true
		page.Entries = page.Entries[:MaxResults]
	}
	if page.More {
		page.NextCursor = memoryEncode(
			[]string{
				digest([]byte(actor + "\x00" + reference)),
				strconv.FormatInt(page.Entries[len(page.Entries)-1].Version, 10),
			},
		)
	}
	for len(page.Entries) > 0 && !memoryFits(page) {
		page.Entries = page.Entries[:len(page.Entries)-1]
		page.More = true
		if len(page.Entries) > 0 {
			page.NextCursor = memoryEncode(
				[]string{
					digest([]byte(actor + "\x00" + reference)),
					strconv.FormatInt(page.Entries[len(page.Entries)-1].Version, 10),
				},
			)
		}
	}
	rows.Close()
	page.Entries, err = s.authorizeMemoryEntries(ctx, actor, page.Entries)
	return page, err
}

func memoryHistoryPosition(actor, reference, cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	var value []string
	if memoryDecode(cursor, &value) != nil || len(value) != 2 || value[0] != digest([]byte(actor+"\x00"+reference)) {
		return 0, invalid()
	}
	version, err := strconv.ParseInt(value[1], 10, 64)
	if err != nil || version < 1 {
		return 0, invalid()
	}
	return version, nil
}

func (s Service) readPrivateMemory(
	ctx context.Context,
	actor string,
	ref memoryReference,
	entry MemoryEntry,
) (MemoryEntry, error) {
	memo, err := s.Memo(ctx, actor, ref.Key)
	if err != nil {
		return entry, err
	}
	if !memo.Active || memo.Version != ref.Version {
		return MemoryEntry{}, conflict("knowledge_stale")
	}
	entry.Text, entry.Active, entry.Phase = memo.Text, memo.Active, MemoryPrivate
	return entry, nil
}

func memoryPrivateTopic(key string) string {
	topic, _, found := strings.Cut(key, ".")
	if !found || topic == "" {
		return "notes"
	}
	return topic
}
