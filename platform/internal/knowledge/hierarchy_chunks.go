package knowledge

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const memoryReadCharacters = 2000

type memoryReadCursor struct {
	Fingerprint string `json:"fingerprint"`
	Offset      int    `json:"offset"`
}

// ReadMemoryPage returns a bounded Unicode chunk from the referenced current
// version. A source edit invalidates every continuation of the older version.
func (s Service) ReadMemoryPage(ctx context.Context, actor, reference, cursor string) (MemoryEntry, error) {
	entry, err := s.readMemoryCurrent(ctx, actor, reference)
	if err != nil {
		return MemoryEntry{}, err
	}
	return memoryChunk(actor, reference, cursor, entry)
}

func memoryChunk(actor, reference, cursor string, entry MemoryEntry) (MemoryEntry, error) {
	fingerprint := digest([]byte(actor + "\x00" + reference))
	position := memoryReadCursor{Fingerprint: fingerprint}
	if cursor != "" &&
		(memoryDecode(cursor, &position) != nil || position.Fingerprint != fingerprint || position.Offset < 0) {
		return MemoryEntry{}, invalid()
	}
	text := []rune(entry.Text)
	if position.Offset > len(text) {
		return MemoryEntry{}, invalid()
	}
	end := min(position.Offset+memoryReadCharacters, len(text))
	entry.Offset, entry.TotalCharacters = position.Offset, len(text)
	entry.Text = string(text[position.Offset:end])
	entry.More = end < len(text)
	if entry.More {
		entry.NextCursor = memoryEncode(memoryReadCursor{fingerprint, end})
	}
	return entry, nil
}

// ReadMemoryRevisionPage is explicitly historical and does not substitute a
// current record. Deletion-scrubbed revisions expose no former body.
func (s Service) ReadMemoryRevisionPage(ctx context.Context, actor, reference, cursor string) (MemoryEntry, error) {
	ref, err := decodeMemoryReference(reference)
	if err != nil {
		return MemoryEntry{}, err
	}
	if err = s.knownActor(ctx, actor); err != nil {
		return MemoryEntry{}, err
	}
	if ref.SourceKind == MemorySourceKind {
		if err = s.sourceReferenceCurrent(ctx, ref); err != nil {
			return MemoryEntry{}, err
		}
	}
	owner := ""
	if ref.Namespace == MemoryPrivate {
		owner = actor
	}
	entry := MemoryEntry{
		Ref:        reference,
		Namespace:  ref.Namespace,
		Event:      ref.Event,
		Topic:      ref.Topic,
		Key:        ref.Key,
		SourceKind: ref.SourceKind,
		Version:    ref.Version,
		Untrusted:  true,
		Historical: true,
		Phase:      "revision",
	}
	err = s.DB.QueryRow(ctx, `SELECT body,active,captured_at FROM core.memory_revisions WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND source_kind=$6 AND version=$7
 AND ($6<>'source' OR EXISTS(SELECT 1 FROM core.assistant_source_documents d JOIN core.assistant_sources s ON s.slot=d.slot AND s.identity=d.identity AND s.digest<>'' WHERE d.slot=$4 AND d.item_key=$5))`, ref.Namespace, owner, ref.Event, ref.Topic, ref.Key, ref.SourceKind, ref.Version).
		Scan(&entry.Text, &entry.Active, &entry.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoryEntry{}, missing()
	}
	if err != nil {
		return MemoryEntry{}, err
	}
	return memoryChunk(actor, reference, cursor, entry)
}
