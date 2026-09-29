package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

// Event precedence is resolved before any search expression is evaluated.
const memoryCandidates = `WITH candidates AS (
 SELECT 'shared'::text AS namespace,f.scope AS event,f.topic,f.fact_key AS item_key,f.body,f.version,
 CASE WHEN f.scope='' THEN 'general' WHEN e.finishes_at<=statement_timestamp() THEN 'past' ELSE 'active_or_upcoming' END AS phase,
 f.scope<>'' AND f.scope<>$2 AS fallback,
 row_number() OVER(PARTITION BY f.topic,f.fact_key ORDER BY
 CASE WHEN f.scope=$2 THEN 0 WHEN f.scope<>'' THEN 1 ELSE 2 END,e.finishes_at DESC NULLS LAST,f.scope) AS priority
 FROM core.knowledge_facts f LEFT JOIN core.events e ON e.id=f.scope
 WHERE f.active AND ($3='' OR f.topic=$3) AND
 (f.scope='' OR f.scope=$2 OR ($2<>'' AND e.finishes_at<=statement_timestamp()
 AND e.finishes_at<=(SELECT finishes_at FROM core.events WHERE id=$2)))
), resolved AS (
 SELECT namespace,event,topic,item_key,body,version,phase,fallback,'fact'::text AS source_kind FROM candidates WHERE priority=1 AND $4<>'private'
 UNION ALL
 SELECT 'private','',CASE WHEN position('.' IN memo_key)>1 THEN split_part(memo_key,'.',1) ELSE 'notes' END,
 memo_key,body,version,'private',false,'memo' FROM core.knowledge_memos
 WHERE owner=$1 AND active AND $4<>'shared'
 AND ($3='' OR $3=CASE WHEN position('.' IN memo_key)>1 THEN split_part(memo_key,'.',1) ELSE 'notes' END)
 UNION ALL
 SELECT 'private','',topic,document_key,body,version,'private',false,'document' FROM core.memory_documents
 WHERE owner=$1 AND active AND $4<>'shared' AND ($3='' OR topic=$3)
 UNION ALL
 SELECT 'shared','',d.slot,d.item_key,d.body,d.version,'source',false,'source'
 FROM core.assistant_source_documents d JOIN core.assistant_sources s ON s.slot=d.slot AND s.identity=d.identity AND s.digest<>''
 WHERE $4<>'private' AND ($3='' OR d.slot=$3)
) `

type memoryCursor struct {
	Fingerprint string `json:"fingerprint"`
	Namespace   string `json:"namespace"`
	Topic       string `json:"topic"`
	Key         string `json:"key"`
	SourceKind  string `json:"source_kind"`
}

type memoryReference struct {
	Namespace      string `json:"namespace"`
	Event          string `json:"event"`
	Topic          string `json:"topic"`
	Key            string `json:"key"`
	Version        int64  `json:"version"`
	RequestedEvent string `json:"requested_event"`
	SourceKind     string `json:"source_kind"`
}

func memoryEncode(value any) string {
	data, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(data)
}

func memoryDecode(value string, target any) error {
	const maxToken = 2048
	if len(value) > maxToken {
		return invalid()
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, target) != nil {
		return invalid()
	}
	return nil
}

func normalizeMemoryQuery(q MemoryQuery) (MemoryQuery, error) {
	if q.Namespace == "" {
		q.Namespace = MemoryAll
	}
	if q.Mode == "" {
		q.Mode = MemoryLiteral
	}
	const maxSearch = 200
	if (q.Namespace != MemoryShared && q.Namespace != MemoryPrivate && q.Namespace != MemoryAll) ||
		(q.Mode != MemoryLiteral && q.Mode != MemoryRegex && q.Mode != MemoryText) ||
		!validID(q.Event, true) || !validID(q.Topic, true) || !validText(q.Text, maxSearch, true) {
		return q, invalid()
	}
	return q, nil
}

func memoryPosition(actor string, q MemoryQuery) (memoryCursor, error) {
	token := q.Cursor
	q.Cursor = ""
	data, _ := json.Marshal(q)
	cursor := memoryCursor{Fingerprint: digest(append([]byte(actor+"\x00"), data...))}
	if token == "" {
		return cursor, nil
	}
	var previous memoryCursor
	if memoryDecode(token, &previous) != nil || previous.Fingerprint != cursor.Fingerprint ||
		(previous.Namespace != MemoryShared && previous.Namespace != MemoryPrivate) ||
		!validID(previous.Topic, false) || !validID(previous.Key, false) {
		return cursor, invalid()
	}
	return previous, nil
}

func (s Service) validateMemoryActor(ctx context.Context, actor string, q MemoryQuery) error {
	if err := s.knownActor(ctx, actor); err != nil {
		return err
	}
	if q.Event == "" {
		return nil
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.events WHERE id=$1)`, q.Event).
		Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return missing()
	}
	return nil
}

// SearchMemory scans bounded candidates. Follow NextCursor even for an empty
// incomplete page; regex scans can exhaust their budget before finding a match.
func (s Service) SearchMemory(ctx context.Context, actor string, query MemoryQuery) (MemoryPage, error) {
	q, err := normalizeMemoryQuery(query)
	if err != nil {
		return MemoryPage{}, err
	}
	cursor, err := memoryPosition(actor, q)
	if err != nil {
		return MemoryPage{}, err
	}
	var pattern *regexp.Regexp
	if q.Mode == MemoryRegex {
		pattern, err = regexp.Compile(q.Text)
		if err != nil {
			return MemoryPage{}, invalid()
		}
	}
	if err = s.validateMemoryActor(ctx, actor, q); err != nil {
		return MemoryPage{}, err
	}
	rows, err := s.DB.Query(ctx, memoryCandidates+`
 SELECT namespace,event,topic,item_key,body,version,phase,fallback,source_kind,
 CASE WHEN $8='text' THEN to_tsvector('simple',body || ' ' || topic || ' ' || item_key) @@ websearch_to_tsquery('simple',$9) ELSE false END
 FROM resolved WHERE (namespace,topic,item_key,source_kind)>($5,$6,$7,$11)
 ORDER BY namespace,topic,item_key,source_kind LIMIT $10`, actor, q.Event, q.Topic, q.Namespace, cursor.Namespace, cursor.Topic, cursor.Key, q.Mode, q.Text, memoryScanLimit+1, cursor.SourceKind)
	if err != nil {
		return MemoryPage{}, err
	}
	defer rows.Close()
	page := MemoryPage{Entries: []MemoryEntry{}}
	for rows.Next() {
		if page.Scanned == memoryScanLimit || len(page.Entries) == MaxResults {
			page.More = true
			page.Incomplete = page.Scanned == memoryScanLimit
			break
		}
		entry := MemoryEntry{Untrusted: true, Active: true}
		var textMatch bool
		if err = rows.Scan(
			&entry.Namespace,
			&entry.Event,
			&entry.Topic,
			&entry.Key,
			&entry.Text,
			&entry.Version,
			&entry.Phase,
			&entry.HistoricalFallback,
			&entry.SourceKind,
			&textMatch,
		); err != nil {
			return MemoryPage{}, err
		}
		page.Scanned++
		next := cursor
		next.Namespace, next.Topic, next.Key, next.SourceKind = entry.Namespace, entry.Topic, entry.Key, entry.SourceKind
		if memoryMatches(entry, q, pattern, textMatch) {
			entry.Ref = memoryEncode(
				memoryReference{
					Namespace:      entry.Namespace,
					Event:          entry.Event,
					Topic:          entry.Topic,
					Key:            entry.Key,
					Version:        entry.Version,
					RequestedEvent: q.Event,
					SourceKind:     entry.SourceKind,
				},
			)
			entry.Text = memoryExcerpt(entry.Text)
			candidate := page
			candidate.Entries = append(candidate.Entries, entry)
			candidate.NextCursor = memoryEncode(next)
			if !memoryFits(candidate) {
				page.More = true
				break
			}
			page = candidate
		}
		cursor = next
		page.NextCursor = memoryEncode(cursor)
	}
	if !page.More {
		page.NextCursor = ""
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	rows.Close()
	page.Entries, err = s.authorizeMemoryEntries(ctx, actor, page.Entries)
	return page, err
}

func memoryMatches(entry MemoryEntry, q MemoryQuery, pattern *regexp.Regexp, textMatch bool) bool {
	if q.Text == "" {
		return true
	}
	text := entry.Topic + "\n" + entry.Key + "\n" + entry.Text
	switch q.Mode {
	case MemoryRegex:
		return pattern.MatchString(text)
	case MemoryText:
		return textMatch
	default:
		return strings.Contains(strings.ToLower(text), strings.ToLower(q.Text))
	}
}

func memoryExcerpt(value string) string {
	runes := []rune(value)
	if len(runes) <= memorySnippetLimit {
		return value
	}
	return string(runes[:memorySnippetLimit]) + "…"
}
