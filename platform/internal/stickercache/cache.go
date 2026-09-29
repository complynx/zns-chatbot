// Package stickercache stores canonical asset descriptions with decayed popularity.
// It never stores message captions, users, media bytes or conversation context.
package stickercache

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Kind string

const (
	Sticker             Kind = "sticker"
	CustomEmoji         Kind = "custom_emoji"
	DefaultCapacity          = 5000
	DefaultHalfLife          = 7 * 24 * time.Hour
	MaxDescriptionBytes      = 8192
	maxAssetIDBytes          = 512
	maxVersionBytes          = 128
	cleanupTimeout           = 3 * time.Second
)

var (
	ErrConfiguration = errors.New("invalid sticker cache configuration")
	ErrKey           = errors.New("invalid sticker cache key")
	ErrDescription   = errors.New("invalid canonical asset description")
)

// Key identifies immutable asset content and the canonical description schema/model version.
// AssetID must be Telegram's stable file_unique_id or custom emoji ID, never a user/message ID.
type Key struct {
	Kind              Kind
	AssetID           string
	DescriptorVersion string
}

// Options must match across processes sharing a cache. Zero values use defaults.
// Now is optional and must be safe for concurrent calls; production uses [time.Now].
type Options struct {
	Capacity int
	HalfLife time.Duration
	Now      func() time.Time
}

type Cache struct {
	db       *pgxpool.Pool
	capacity int
	halfLife time.Duration
	now      func() time.Time
}

// Processor describes only the asset identified by Key. Implementations must not
// include captions, sender identity or conversation-dependent interpretations.
type Processor func(context.Context, Key) (string, error)

func New(db *pgxpool.Pool, options Options) (*Cache, error) {
	if options.Capacity == 0 {
		options.Capacity = DefaultCapacity
	}
	if options.HalfLife == 0 {
		options.HalfLife = DefaultHalfLife
	}
	if db == nil || options.Capacity < 1 || options.HalfLife < time.Second {
		return nil, ErrConfiguration
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Cache{db: db, capacity: options.Capacity, halfLife: options.HalfLife, now: options.Now}, nil
}

func validKey(key Key) bool {
	return (key.Kind == Sticker || key.Kind == CustomEmoji) && validText(key.AssetID, maxAssetIDBytes) &&
		validText(key.DescriptorVersion, maxVersionBytes)
}

func validText(value string, limit int) bool {
	return len(value) <= limit && strings.TrimSpace(value) != "" && utf8.ValidString(value) &&
		!strings.ContainsRune(value, 0)
}

func lockKey(key Key) int64 {
	digest := sha256.Sum256([]byte(string(key.Kind) + "\x00" + key.AssetID + "\x00" + key.DescriptorVersion))
	return int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
}

// release never returns a connection with a possibly held advisory lock to the pool.
func release(conn *pgxpool.Conn, key int64) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	var unlocked bool
	err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Hijack().Close(ctx)
		return
	}
	conn.Release()
}

// Get returns a cached description or processes a miss. Same-key work is serialized
// across processes. The callback holds one pool connection but no transaction.
// The pool bounds outstanding processors; callbacks must not acquire from this pool.
func (c *Cache) Get(ctx context.Context, key Key, process Processor) (string, error) {
	if !validKey(key) {
		return "", ErrKey
	}
	conn, err := c.db.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire sticker cache connection: %w", err)
	}
	lock := lockKey(key)
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lock); err != nil {
		// Cancellation can race server-side acquisition; destroy rather than pool it.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_ = conn.Hijack().Close(cleanupCtx)
		return "", fmt.Errorf("lock sticker asset: %w", err)
	}
	defer release(conn, lock)
	description, err := c.touch(ctx, conn, key)
	if err != nil || description != "" {
		return description, err
	}
	if process == nil {
		return "", ErrConfiguration
	}
	description, err = process(ctx, key)
	if err != nil {
		return "", fmt.Errorf("describe sticker asset: %w", err)
	}
	if !validText(description, MaxDescriptionBytes) {
		return "", ErrDescription
	}
	if err = c.publish(ctx, conn, key, description); err != nil {
		return "", err
	}
	return description, nil
}

func (c *Cache) touch(ctx context.Context, conn *pgxpool.Conn, key Key) (string, error) {
	var description *string
	err := conn.QueryRow(ctx, `WITH guard AS MATERIALIZED (
		SELECT pg_advisory_xact_lock_shared(183627, 1)
	) INSERT INTO bot.sticker_descriptions
		(kind,asset_id,descriptor_version,score,scored_at) SELECT $1,$2,$3,1,$4 FROM guard
		ON CONFLICT(kind,asset_id,descriptor_version) DO UPDATE SET
		score=(CASE WHEN extract(epoch FROM ($4::timestamptz-bot.sticker_descriptions.scored_at))/$5 >= 1022
		 THEN 0 ELSE bot.sticker_descriptions.score * power(0.5,
		 greatest(0,extract(epoch FROM ($4::timestamptz-bot.sticker_descriptions.scored_at)))/$5) END)+1,
		scored_at=greatest(bot.sticker_descriptions.scored_at,$4)
		RETURNING description`, key.Kind, key.AssetID, key.DescriptorVersion, c.now(), c.halfLife.Seconds()).Scan(&description)
	if err != nil {
		return "", fmt.Errorf("count sticker access: %w", err)
	}
	if description == nil {
		return "", nil
	}
	return *description, nil
}

func (c *Cache) publish(ctx context.Context, conn *pgxpool.Conn, key Key, description string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin sticker admission: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()
	// Two-integer advisory keys have a separate namespace from per-asset bigint keys.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(183627, 1)"); err != nil {
		return fmt.Errorf("lock sticker admission: %w", err)
	}
	now := c.now()
	if err = c.evict(ctx, tx, now); err != nil {
		return err
	}
	boost, err := c.admissionScore(ctx, tx, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE bot.sticker_descriptions SET description=$4,
		score=greatest(CASE WHEN extract(epoch FROM ($5::timestamptz-scored_at))/$6 >= 1022 THEN 0
		 ELSE score*power(0.5,greatest(0,extract(epoch FROM ($5::timestamptz-scored_at)))/$6) END,$7),
		scored_at=greatest(scored_at,$5)
		WHERE kind=$1 AND asset_id=$2 AND descriptor_version=$3`,
		key.Kind, key.AssetID, key.DescriptorVersion, description, now, c.halfLife.Seconds(), boost)
	if err != nil {
		return fmt.Errorf("publish sticker description: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sticker admission: %w", err)
	}
	return nil
}

func (c *Cache) admissionScore(ctx context.Context, tx pgx.Tx, now time.Time) (float64, error) {
	var threshold float64
	err := tx.QueryRow(ctx, `SELECT CASE WHEN extract(epoch FROM ($1::timestamptz-scored_at))/$2 >= 1022 THEN 0
		ELSE score * power(0.5,greatest(0,extract(epoch FROM ($1::timestamptz-scored_at)))/$2) END
		FROM bot.sticker_descriptions WHERE description IS NOT NULL
		ORDER BY 1 DESC,kind,asset_id,descriptor_version
		OFFSET (SELECT count(*)/2 FROM bot.sticker_descriptions WHERE description IS NOT NULL) LIMIT 1`,
		now, c.halfLife.Seconds()).Scan(&threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("rank sticker admission: %w", err)
	}
	return max(threshold+1, math.Nextafter(threshold, math.Inf(1))), nil
}

func (c *Cache) evict(ctx context.Context, tx pgx.Tx, now time.Time) error {
	var residents int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT 1 FROM bot.sticker_descriptions WHERE description IS NOT NULL LIMIT $1
	) AS residents`, c.capacity).Scan(&residents)
	if err != nil {
		return fmt.Errorf("count sticker residents: %w", err)
	}
	if residents < c.capacity {
		return nil
	}
	_, err = tx.Exec(ctx, `WITH victims AS (
		SELECT kind,asset_id,descriptor_version FROM bot.sticker_descriptions
		WHERE description IS NOT NULL
		ORDER BY CASE WHEN extract(epoch FROM ($1::timestamptz-scored_at))/$2 >= 1022 THEN 0
		ELSE score * power(0.5,greatest(0,extract(epoch FROM ($1::timestamptz-scored_at)))/$2) END DESC,
		kind,asset_id,descriptor_version OFFSET $3
	) UPDATE bot.sticker_descriptions AS cache SET description=NULL FROM victims
	WHERE cache.kind=victims.kind AND cache.asset_id=victims.asset_id
	AND cache.descriptor_version=victims.descriptor_version`, now, c.halfLife.Seconds(), c.capacity-1)
	if err != nil {
		return fmt.Errorf("evict sticker descriptions: %w", err)
	}
	return nil
}
