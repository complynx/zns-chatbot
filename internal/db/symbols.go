package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/symbols"
)

const defaultSymbolLimit = 5000
const defaultSymbolHalfLife = 30 * 24 * 60 * 60

func (d Settings) SymbolPolicy() (int, int, int, error) {
	sticker, emoji, half := d.StickerCatalogLimit, d.EmojiCatalogLimit, d.SymbolHalfLifeSeconds
	if sticker == 0 {
		sticker = defaultSymbolLimit
	}
	if emoji == 0 {
		emoji = defaultSymbolLimit
	}
	if half == 0 {
		half = defaultSymbolHalfLife
	}
	if sticker < 1 || sticker > 50000 || emoji < 1 || emoji > 50000 || half < 60 || half > 365*24*60*60 {
		return 0, 0, 0, errors.New("invalid visual symbol catalog policy")
	}
	if _, err := d.symbolDecayPolicy(half); err != nil {
		return 0, 0, 0, err
	}
	return sticker, emoji, half, nil
}

// Intake's duplicate-update gate ensures exactly one increment per accepted occurrence.
// A deployment lock also makes eviction atomic across concurrent conversations.
func touchSymbols(ctx context.Context, tx pgx.Tx, bot int64, taskID uuid.UUID, items []symbols.Occurrence) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) > symbols.MaxOccurrences {
		return symbols.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))",
		fmt.Sprintf("symbols:%d", bot)); err != nil {
		return err
	}
	var raw []byte
	var settings Settings
	if err := tx.QueryRow(ctx, "SELECT document FROM shuttle_settings WHERE id").Scan(&raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return err
	}
	stickers, emojis, half, err := settings.SymbolPolicy()
	if err != nil {
		return err
	}
	halves, err := symbolHalfLives(ctx, tx, bot, taskID, settings, half)
	if err != nil {
		return err
	}
	for _, o := range items {
		if !o.Reference.Valid() {
			return symbols.ErrInvalid
		}
		ref, marshalErr := json.Marshal(o.Reference)
		if marshalErr != nil {
			return marshalErr
		}
		half = halves[o.Kind]
		prior, priorErr := historicSymbolScore(ctx, tx, bot, taskID, o.Reference, half)
		if priorErr != nil {
			return priorErr
		}
		_, err = tx.Exec(ctx, `INSERT INTO shuttle_symbols(bot_id,kind,asset_key,generation,reference,score,last_used_at)
VALUES($1,$2,$3,$4,$5,$7,transaction_timestamp())
ON CONFLICT(bot_id,kind,asset_key) DO UPDATE SET
score=shuttle_symbol_weight(shuttle_symbols.score,extract(epoch FROM
 (transaction_timestamp()-shuttle_symbols.last_used_at))::float8,$6)+1,
last_used_at=greatest(shuttle_symbols.last_used_at,transaction_timestamp()),
last_interval_seconds=CASE WHEN transaction_timestamp()>shuttle_symbols.last_used_at
 THEN extract(epoch FROM (transaction_timestamp()-shuttle_symbols.last_used_at))::float8
 ELSE shuttle_symbols.last_interval_seconds END,
reference=shuttle_symbols.reference||EXCLUDED.reference`, bot, o.Kind, o.ID, uuid.New(), ref, half, prior+1)
		if err != nil {
			return err
		}
	}
	for kind, limit := range map[string]int{symbols.Sticker: stickers, symbols.Emoji: emojis} {
		half = halves[kind]
		_, err = tx.Exec(ctx, `DELETE FROM shuttle_symbols WHERE bot_id=$1 AND kind=$2 AND asset_key IN (
 SELECT asset_key FROM shuttle_symbols WHERE bot_id=$1 AND kind=$2
 ORDER BY ln(score)-ln(2.0)*greatest(0,extract(epoch FROM
 (transaction_timestamp()-last_used_at)))/$3::float8 DESC,last_used_at DESC,asset_key
 OFFSET $4)`, bot, kind, half, limit)
		if err != nil {
			return err
		}
	}
	return nil
}

// Retained inbox history preserves popularity after description-cache eviction.
// A frequently reused newcomer can therefore enter a full catalog instead of
// losing its count on every cache miss. Cache hits need no history scan.
func historicSymbolScore(
	ctx context.Context,
	tx pgx.Tx,
	bot int64,
	task uuid.UUID,
	r symbols.Reference,
	half int,
) (float64, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_symbols
WHERE bot_id=$1 AND kind=$2 AND asset_key=$3)`, bot, r.Kind, r.ID).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, nil
	}
	needle, _ := json.Marshal(map[string]any{"symbols": []map[string]string{{"kind": r.Kind, "id": r.ID}}})
	var score float64
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(shuttle_symbol_weight(1.0,extract(epoch FROM
 (transaction_timestamp()-t.created_at))::float8,$5)),0)::float8
FROM shuttle_tasks t JOIN shuttle_users u ON u.id=t.user_id
CROSS JOIN LATERAL jsonb_array_elements(t.request->'symbols') o
WHERE u.bot_id=$1 AND t.id<>$2 AND t.request @> $3::jsonb
AND o->>'kind'=$4 AND o->>'id'=$6`, bot, task, needle, r.Kind, half, r.ID).Scan(&score)
	return score, err
}

// SymbolDescriptions reads only descriptions, never raw Telegram identifiers into the prompt.
func (s *Store) SymbolDescriptions(ctx context.Context, bot int64, items []symbols.Occurrence) (
	map[string]symbols.Description, error,
) {
	result := map[string]symbols.Description{}
	for _, o := range items {
		key := o.Kind + ":" + o.ID
		if _, ok := result[key]; ok {
			continue
		}
		var raw []byte
		err := s.pool.QueryRow(ctx, `SELECT description FROM shuttle_symbols
WHERE bot_id=$1 AND kind=$2 AND asset_key=$3 AND description IS NOT NULL`, bot, o.Kind, o.ID).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var d symbols.Description
		if err = json.Unmarshal(raw, &d); err != nil || !d.Valid() {
			return nil, symbols.ErrInvalid
		}
		result[key] = d
	}
	return result, nil
}

// SymbolDescriptionWaitOpen bounds cold-cache waiting from durable intake time.
// PostgreSQL time preserves the same deadline across retries and process restarts.
func (s *Store) SymbolDescriptionWaitOpen(ctx context.Context, bot int64, task uuid.UUID) (bool, error) {
	var open bool
	err := s.pool.QueryRow(ctx, `SELECT now() < t.created_at + interval '6 minutes'
FROM shuttle_tasks t JOIN shuttle_users u ON u.id=t.user_id
WHERE t.id=$1 AND u.bot_id=$2`, task, bot).Scan(&open)
	return open, err
}

type SymbolRecord struct {
	Reference  symbols.Reference
	Generation uuid.UUID
	Workflow   json.RawMessage
	Described  bool
}

// PendingSymbols pages the retained catalog, not history. Completed entries never start another description.
func (s *Store) PendingSymbols(ctx context.Context, bot int64, after uuid.UUID) ([]SymbolRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT generation,reference,workflow FROM shuttle_symbols
WHERE bot_id=$1 AND description IS NULL AND generation>$2 ORDER BY generation LIMIT 100`, bot, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SymbolRecord
	for rows.Next() {
		var item SymbolRecord
		var raw []byte
		if err = rows.Scan(&item.Generation, &raw, &item.Workflow); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item.Reference); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// PendingSymbol returns a retained cache entry for the separate descriptor procedure.
func (s *Store) PendingSymbol(ctx context.Context, bot int64, kind, key string) (SymbolRecord, error) {
	var r SymbolRecord
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT generation,reference,workflow,description IS NOT NULL FROM shuttle_symbols
WHERE bot_id=$1 AND kind=$2 AND asset_key=$3`, bot, kind, key).
		Scan(&r.Generation, &raw, &r.Workflow, &r.Described)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r.Reference)
	return r, err
}

// SaveSymbolWorkflow is a CAS checkpoint. Eviction/re-admission cannot adopt an old paid run.
func (s *Store) SaveSymbolWorkflow(
	ctx context.Context,
	bot int64,
	r SymbolRecord,
	previous, next json.RawMessage,
) error {
	result, err := s.pool.Exec(ctx, `UPDATE shuttle_symbols SET workflow=$5
WHERE bot_id=$1 AND kind=$2 AND asset_key=$3 AND generation=$4 AND workflow=$6 AND description IS NULL`,
		bot, r.Reference.Kind, r.Reference.ID, r.Generation, next, previous)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) CompleteSymbolDescription(ctx context.Context, bot int64, r SymbolRecord, d symbols.Description,
	workflow json.RawMessage,
) error {
	if !d.Valid() {
		return symbols.ErrInvalid
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE shuttle_symbols SET description=$5
WHERE bot_id=$1 AND kind=$2 AND asset_key=$3 AND generation=$4 AND workflow=$6
 AND (description IS NULL OR description=$5)`,
		bot, r.Reference.Kind, r.Reference.ID, r.Generation, raw, workflow)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
