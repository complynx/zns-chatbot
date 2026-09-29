package knowledge

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// DocumentState returns only owner-scoped mutation metadata. New keys have
// version zero; inactive keys retain the version needed for safe replacement.
func (s Service) DocumentState(ctx context.Context, actor, topic, key string) (Document, error) {
	if !validID(topic, false) || !validID(key, false) {
		return Document{}, invalid()
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return Document{}, err
	}
	result := Document{Topic: topic, Key: key}
	err := s.DB.QueryRow(ctx, `SELECT version,active FROM core.memory_documents WHERE owner=$1 AND topic=$2 AND document_key=$3`, actor, topic, key).
		Scan(&result.Version, &result.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	return result, err
}

func writeDocument(ctx context.Context, tx pgx.Tx, actor string, c Command) (Result, error) {
	var version int64
	err := tx.QueryRow(ctx, `SELECT version FROM core.memory_documents WHERE owner=$1 AND topic=$2 AND document_key=$3`, actor, c.Topic, c.FactKey).
		Scan(&version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if version != c.Version {
		return Result{}, conflict("knowledge_stale")
	}
	if c.Name == DocumentDelete && version == 0 {
		return Result{}, missing()
	}
	if c.Name == DocumentSet {
		var count int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM core.memory_documents WHERE owner=$1 AND active AND (topic,document_key)<>($2,$3)`, actor, c.Topic, c.FactKey).
			Scan(&count)
		if err != nil {
			return Result{}, err
		}
		if count >= MaxDocuments {
			return Result{}, conflict("knowledge_document_capacity")
		}
	}
	document := Document{
		Topic:   c.Topic,
		Key:     c.FactKey,
		Text:    c.Text,
		Version: version + 1,
		Active:  c.Name == DocumentSet,
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.memory_documents(owner,topic,document_key,body,version,active) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(owner,topic,document_key) DO UPDATE SET body=excluded.body,version=excluded.version,active=excluded.active`,
		actor,
		c.Topic,
		c.FactKey,
		c.Text,
		document.Version,
		document.Active,
	)
	return Result{Document: &document}, err
}

func (s Service) readDocument(
	ctx context.Context,
	actor string,
	ref memoryReference,
	entry MemoryEntry,
) (MemoryEntry, error) {
	if err := s.knownActor(ctx, actor); err != nil {
		return entry, err
	}
	var version int64
	err := s.DB.QueryRow(ctx, `SELECT body,version,active FROM core.memory_documents WHERE owner=$1 AND topic=$2 AND document_key=$3`, actor, ref.Topic, ref.Key).
		Scan(&entry.Text, &version, &entry.Active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!entry.Active || version != ref.Version)) {
		return MemoryEntry{}, conflict("knowledge_stale")
	}
	entry.Phase = MemoryPrivate
	return entry, err
}
