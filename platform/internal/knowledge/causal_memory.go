package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type memoryCausalRecord struct {
	entry   MemoryEntry
	refs    []readsource.Authority
	revoked bool
	found   bool
}

func memoryEntryOwner(actor string, entry MemoryEntry) string {
	if entry.Namespace == MemoryPrivate {
		return actor
	}
	return ""
}

func loadMemoryCausal(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	entry MemoryEntry,
	lock bool,
) (memoryCausalRecord, error) {
	record := memoryCausalRecord{entry: entry}
	query := `SELECT a.authorities,COALESCE(a.revoked,false),r.origin FROM core.memory_revisions r LEFT JOIN core.memory_read_authorities a USING(namespace,owner,scope,topic,item_key,source_kind,version) WHERE r.namespace=$1 AND r.owner=$2 AND r.scope=$3 AND r.topic=$4 AND r.item_key=$5 AND r.source_kind=$6 AND r.version=$7`
	if lock {
		query += " FOR UPDATE OF r"
	}
	var raw []byte
	var origin string
	err := tx.QueryRow(ctx, query, entry.Namespace, memoryEntryOwner(actor, entry), entry.Event, entry.Topic, entry.Key, entry.SourceKind, entry.Version).
		Scan(&raw, &record.revoked, &origin)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	if origin == "original" && raw == nil {
		return record, nil
	}
	record.found = true
	if origin != "derived" || raw == nil {
		return record, errors.New("missing memory causal authority")
	}
	if err = json.Unmarshal(raw, &record.refs); err != nil {
		return record, err
	}
	if record.refs == nil || !readsource.Valid(record.refs) {
		return record, errors.New("invalid memory causal authority")
	}
	for _, ref := range record.refs {
		if ref.Causal == nil {
			return record, errors.New("invalid memory causal origin")
		}
	}
	return record, nil
}

// authorizeMemoryEntries preserves each immutable body/version snapshot while
// validating its selected causal records. Denial for one reader is not deletion.
func (s Service) authorizeMemoryEntries(
	ctx context.Context,
	actor string,
	entries []MemoryEntry,
) ([]MemoryEntry, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	records := make([]memoryCausalRecord, len(entries))
	all := []readsource.Authority{}
	for i, entry := range entries {
		records[i], err = loadMemoryCausal(ctx, tx, actor, entry, false)
		if err != nil {
			return nil, err
		}
		all, err = readsource.Merge(all, records[i].refs)
		if err != nil {
			return nil, err
		}
	}
	if err = readsource.LockEvents(ctx, tx, all); err != nil {
		return nil, err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor}, all); err != nil {
		return nil, err
	}
	validity, err := readsource.LockValidity(ctx, tx, actor, all)
	if err != nil {
		return nil, err
	}
	// Stable order also covers several versions of one record in revision pages.
	order := make([]int, len(records))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(i, j int) int { return compareMemoryEntries(records[i].entry, records[j].entry) })
	allowed := make([]bool, len(records))
	for _, i := range order {
		record := records[i]
		if !record.found {
			allowed[i] = true
			continue
		}
		allowed[i], err = authorizeMemoryRecord(ctx, tx, actor, record, all, validity)
		if err != nil {
			return nil, err
		}
	}
	result := []MemoryEntry{}
	for i, entry := range entries {
		if !allowed[i] {
			continue
		}
		entry, err = memoryEntryEvidence(actor, entry, records[i])
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, tx.Commit(ctx)
}

func compareMemoryEntries(a, b MemoryEntry) int {
	x := []string{a.Namespace, a.Event, a.Topic, a.Key, a.SourceKind}
	y := []string{b.Namespace, b.Event, b.Topic, b.Key, b.SourceKind}
	if order := slices.Compare(x, y); order != 0 {
		return order
	}
	if a.Version < b.Version {
		return -1
	}
	if a.Version > b.Version {
		return 1
	}
	return 0
}

func memoryCausalValidity(refs, all []readsource.Authority, validity readsource.Validity) (bool, bool) {
	origin, reader := true, true
	for _, ref := range refs {
		index := slices.IndexFunc(all, func(a readsource.Authority) bool { return readsource.Equal(ref, a) })
		if index < 0 {
			return false, false
		}
		origin = origin && validity.Origin[index]
		reader = reader && validity.Reader[index]
	}
	return origin, reader
}

func revokeMemoryCausal(ctx context.Context, tx pgx.Tx, actor string, entry MemoryEntry) error {
	_, err := tx.Exec(
		ctx,
		`UPDATE core.memory_read_authorities SET revoked=true WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND source_kind=$6 AND version=$7`,
		entry.Namespace,
		memoryEntryOwner(actor, entry),
		entry.Event,
		entry.Topic,
		entry.Key,
		entry.SourceKind,
		entry.Version,
	)
	return err
}

func bindMemoryCausal(ctx context.Context, tx pgx.Tx, actor string, result Result, source readsource.Derivation) error {
	refs, err := readsource.Capture(actor, source)
	if err != nil {
		return err
	}
	if result.Proposal != nil && result.Fact == nil {
		return bindProposalCausal(ctx, tx, result.Proposal, refs)
	}
	return bindMemoryCausalRefs(ctx, tx, actor, result, refs)
}

func bindMemoryCausalRefs(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	result Result,
	refs []readsource.Authority,
) error {
	ref, ok := resultMemoryReference(result)
	if !ok {
		return nil
	}
	owner := ""
	if ref.Namespace == MemoryPrivate {
		owner = actor
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE core.memory_revisions SET origin='derived' WHERE namespace=$1 AND owner=$2 AND scope=$3 AND topic=$4 AND item_key=$5 AND source_kind=$6 AND version=$7`,
		ref.Namespace,
		owner,
		ref.Event,
		ref.Topic,
		ref.Key,
		ref.SourceKind,
		ref.Version,
	); err != nil {
		return err
	}
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.memory_read_authorities(namespace,owner,scope,topic,item_key,source_kind,version,authorities) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		ref.Namespace,
		owner,
		ref.Event,
		ref.Topic,
		ref.Key,
		ref.SourceKind,
		ref.Version,
		refs,
	)
	if err != nil {
		return err
	}
	leaf := knowledgeauthority.ReadAuthority{
		Kind:       knowledgeauthority.DerivedMemory,
		Namespace:  ref.Namespace,
		Owner:      owner,
		Scope:      ref.Event,
		Topic:      ref.Topic,
		Key:        ref.Key,
		SourceKind: ref.SourceKind,
		Version:    ref.Version,
	}
	evidence, err := readsource.Merge([]readsource.Authority{{Knowledge: leaf}})
	if err != nil {
		return err
	}
	if result.Fact != nil {
		result.Fact.ReadAuthorities = evidence
	}
	if result.Memo != nil {
		result.Memo.ReadAuthorities = evidence
	}
	if result.Document != nil {
		result.Document.ReadAuthorities = evidence
	}
	return nil
}

func (s Service) authorizeMemoryEntry(ctx context.Context, actor string, entry MemoryEntry) (MemoryEntry, error) {
	entries, err := s.authorizeMemoryEntries(ctx, actor, []MemoryEntry{entry})
	if err != nil {
		return MemoryEntry{}, err
	}
	if len(entries) != 1 {
		return MemoryEntry{}, conflict("knowledge_stale")
	}
	return entries[0], nil
}

// A review creates a fact revision, but does not create another proposal body.
func bindMemoryCausalRefsFromDerivation(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	result Result,
	source readsource.Derivation,
) error {
	refs, err := readsource.Capture(actor, source)
	if err != nil {
		return err
	}
	return bindMemoryCausalRefs(ctx, tx, actor, result, refs)
}

func authorizeMemoryRecord(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	record memoryCausalRecord,
	all []readsource.Authority,
	validity readsource.Validity,
) (bool, error) {
	record, err := loadMemoryCausal(ctx, tx, actor, record.entry, true)
	if err != nil {
		return false, err
	}
	origin, reader := memoryCausalValidity(record.refs, all, validity)
	if !origin && !record.revoked {
		if err = revokeMemoryCausal(ctx, tx, actor, record.entry); err != nil {
			return false, err
		}
	}
	return origin && reader && !record.revoked, nil
}

func memoryEntryEvidence(actor string, entry MemoryEntry, record memoryCausalRecord) (MemoryEntry, error) {
	var err error
	if record.found {
		leaf := knowledgeauthority.ReadAuthority{
			Kind:       knowledgeauthority.DerivedMemory,
			Namespace:  entry.Namespace,
			Owner:      memoryEntryOwner(actor, entry),
			Scope:      entry.Event,
			Topic:      entry.Topic,
			Key:        entry.Key,
			SourceKind: entry.SourceKind,
			Version:    entry.Version,
		}
		entry.ReadAuthorities, err = readsource.Merge([]readsource.Authority{{Knowledge: leaf}})
		if err != nil {
			return MemoryEntry{}, err
		}
	}
	return entry, nil
}
