package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const MemorySourceKind = "source"
const AssistantQA = "assistant_qa"
const AssistantAbout = "assistant_about"
const MaxSourceBytes = 4 << 20

type SourceProvenance struct {
	Status       string     `json:"status"`
	RefreshedAt  *time.Time `json:"refreshed_at"`
	SourceDigest string     `json:"source_digest"`
	TextDigest   string     `json:"text_digest"`
	CapturedAt   time.Time  `json:"captured_at"`
}

type SourceStatus struct {
	Slot        string     `json:"slot"`
	Status      string     `json:"status"`
	Version     int64      `json:"version"`
	AttemptedAt *time.Time `json:"attempted_at"`
	RefreshedAt *time.Time `json:"refreshed_at"`
	ErrorCode   string     `json:"error_code"`
}

// ConfigureSource switches visibility before fetching. A changed identity cannot
// reuse an old document, including a retained exact/history reference.
func (s Service) ConfigureSource(ctx context.Context, slot, identity string) error {
	if !sourceSlot(slot) || (identity != "" && !validSourceDigest(identity)) {
		return invalid()
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO core.assistant_sources(slot,identity) VALUES($1,$2)
 ON CONFLICT(slot) DO UPDATE SET identity=excluded.identity,
 digest=CASE WHEN assistant_sources.identity=excluded.identity THEN assistant_sources.digest ELSE '' END,
 status=CASE WHEN assistant_sources.identity=excluded.identity THEN assistant_sources.status ELSE 'unavailable' END,
 attempted_at=CASE WHEN assistant_sources.identity=excluded.identity THEN assistant_sources.attempted_at ELSE NULL END,
 refreshed_at=CASE WHEN assistant_sources.identity=excluded.identity THEN assistant_sources.refreshed_at ELSE NULL END,
 error_code=CASE WHEN assistant_sources.identity=excluded.identity THEN assistant_sources.error_code ELSE '' END`, slot, identity)
	return core.DatabaseOperationError(err)
}

func sourceSlot(slot string) bool { return slot == AssistantQA || slot == AssistantAbout }
func validSourceDigest(value string) bool {
	const digestLength = 64
	if len(value) != digestLength {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// ReplaceSource publishes every entry and its revision in one transaction. It
// never writes curated facts, private records, or another configured identity.
func (s Service) ReplaceSource(ctx context.Context, slot, identity, sourceDigest string, texts []string) error {
	if !sourceSlot(slot) || !validSourceDigest(identity) || !validSourceDigest(sourceDigest) {
		return invalid()
	}
	total := 0
	for _, text := range texts {
		total += len(text)
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || total > MaxSourceBytes {
			return invalid()
		}
	}
	const maxSourceEntries = 10000
	if len(texts) > maxSourceEntries {
		return invalid()
	}
	encoded, _ := json.Marshal(texts)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current, previous, previousText string
	var version int64
	err = tx.QueryRow(ctx, `SELECT s.identity,s.digest,s.version,COALESCE(v.text_digest,'') FROM core.assistant_sources s
 LEFT JOIN core.assistant_source_versions v ON v.slot=s.slot AND v.identity=s.identity AND v.version=s.version
 WHERE s.slot=$1 FOR UPDATE OF s`, slot).
		Scan(&current, &previous, &version, &previousText)
	if err != nil {
		// An unconfigured slot keeps its existing pgx.ErrNoRows outcome.
		return core.DatabaseOperationError(err)
	}
	if current != identity {
		return conflict("knowledge_stale")
	}
	if previous == sourceDigest && previousText == digest(encoded) {
		_, err = tx.Exec(
			ctx,
			`UPDATE core.assistant_sources SET status='ready',attempted_at=clock_timestamp(),refreshed_at=clock_timestamp(),error_code='' WHERE slot=$1`,
			slot,
		)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
		return core.DatabaseOperationError(tx.Commit(ctx))
	}
	version++
	if _, err = tx.Exec(
		ctx,
		`DELETE FROM core.assistant_source_documents WHERE slot=$1 AND identity=$2`,
		slot,
		identity,
	); err != nil {
		return core.DatabaseOperationError(err)
	}
	if err = insertSourceDocuments(ctx, tx, slot, identity, version, texts); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.assistant_source_versions(slot,identity,version,source_digest,text_digest) VALUES($1,$2,$3,$4,$5)`,
		slot,
		identity,
		version,
		sourceDigest,
		digest(encoded),
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.assistant_sources SET version=$2,digest=$3,status='ready',attempted_at=clock_timestamp(),refreshed_at=clock_timestamp(),error_code='' WHERE slot=$1`,
		slot,
		version,
		sourceDigest,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func insertSourceDocuments(ctx context.Context, tx pgx.Tx, slot, identity string, version int64, texts []string) error {
	const segmentCharacters = 16000
	for index, text := range texts {
		runes := []rune(text)
		for offset, part := 0, 0; offset < len(runes) || part == 0; part++ {
			end := min(offset+segmentCharacters, len(runes))
			body := string(runes[offset:end])
			key := fmt.Sprintf("%s.%05d.%05d", identity, index, part)
			_, err := tx.Exec(
				ctx,
				`INSERT INTO core.assistant_source_documents(slot,identity,item_key,body,version) VALUES($1,$2,$3,$4,$5)`,
				slot,
				identity,
				key,
				body,
				version,
			)
			if err != nil {
				return core.DatabaseOperationError(err)
			}
			_, err = tx.Exec(
				ctx,
				`INSERT INTO core.memory_revisions(namespace,owner,scope,topic,item_key,source_kind,version,body,active) VALUES('shared','','',$1,$2,'source',$3,$4,true)`,
				slot,
				key,
				version,
				body,
			)
			if err != nil {
				return core.DatabaseOperationError(err)
			}
			offset = end
		}
	}
	return nil
}

// SourceFailure retains last-good content and records only a finite safe code.
func (s Service) SourceFailure(ctx context.Context, slot, identity, code string) error {
	switch code {
	case "fetch_failed", "invalid_source", "source_limit", "source_timeout", "source_canceled", "source_unavailable":
	default:
		return invalid()
	}
	_, err := s.DB.Exec(
		ctx,
		`UPDATE core.assistant_sources SET status=CASE WHEN digest='' THEN 'unavailable' ELSE 'stale' END,attempted_at=clock_timestamp(),error_code=$3 WHERE slot=$1 AND identity=$2`,
		slot,
		identity,
		code,
	)
	return core.DatabaseOperationError(err)
}

func (s Service) SourceStatuses(ctx context.Context) ([]SourceStatus, error) {
	rows, err := s.DB.Query(
		ctx,
		`SELECT slot,status,version,attempted_at,refreshed_at,error_code FROM core.assistant_sources ORDER BY slot`,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	statuses, err := pgx.CollectRows(rows, pgx.RowToStructByPos[SourceStatus])
	return statuses, core.DatabaseOperationError(err)
}

func (s Service) sourceReferenceCurrent(ctx context.Context, ref memoryReference) error {
	if ref.Namespace != MemoryShared || ref.Event != "" || !sourceSlot(ref.Topic) {
		return invalid()
	}
	var exists bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.assistant_source_documents d JOIN core.assistant_sources s ON s.slot=d.slot AND s.identity=d.identity AND s.digest<>'' WHERE d.slot=$1 AND d.item_key=$2)`, ref.Topic, ref.Key).
		Scan(&exists)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !exists {
		return conflict("knowledge_stale")
	}
	return nil
}

func (s Service) readSource(
	ctx context.Context,
	actor string,
	ref memoryReference,
	entry MemoryEntry,
) (MemoryEntry, error) {
	if err := s.knownActor(ctx, actor); err != nil {
		return entry, err
	}
	var provenance SourceProvenance
	err := s.DB.QueryRow(ctx, `SELECT d.body,v.source_digest,v.text_digest,v.captured_at,s.status,s.refreshed_at FROM core.assistant_source_documents d
 JOIN core.assistant_sources s ON s.slot=d.slot AND s.identity=d.identity AND s.digest<>''
 JOIN core.assistant_source_versions v ON v.slot=d.slot AND v.identity=d.identity AND v.version=d.version
 WHERE d.slot=$1 AND d.item_key=$2 AND d.version=$3`, ref.Topic, ref.Key, ref.Version).
		Scan(&entry.Text, &provenance.SourceDigest, &provenance.TextDigest, &provenance.CapturedAt, &provenance.Status, &provenance.RefreshedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoryEntry{}, conflict("knowledge_stale")
	}
	entry.Active = true
	entry.Phase = "source"
	entry.Source = &provenance
	return entry, core.DatabaseOperationError(err)
}
