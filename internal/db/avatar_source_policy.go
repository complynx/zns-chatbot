package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const MaxAvatarBrowserStorageBytes int64 = 10_000_000_000
const MaxAvatarBrowserRetentionSeconds = 90 * 24 * 60 * 60
const MinAvatarBrowserRetentionSeconds = 60

type AvatarBrowserPolicy struct {
	LimitBytes int64
	Retention  time.Duration
}

// AvatarBrowserPolicy defaults old settings to a global 10 GB, 90-day source budget.
// It excludes Telegram references and completed JPEGs.
func (s Settings) AvatarBrowserPolicy() (AvatarBrowserPolicy, error) {
	limit, seconds := s.AvatarBrowserStorageLimitBytes, s.AvatarBrowserRetentionSeconds
	if limit == 0 {
		limit = MaxAvatarBrowserStorageBytes
	}
	if seconds == 0 {
		seconds = MaxAvatarBrowserRetentionSeconds
	}
	if limit < 1 || limit > MaxAvatarBrowserStorageBytes || seconds < MinAvatarBrowserRetentionSeconds ||
		seconds > MaxAvatarBrowserRetentionSeconds {
		return AvatarBrowserPolicy{}, errors.New("invalid browser avatar source policy")
	}
	return AvatarBrowserPolicy{LimitBytes: limit, Retention: time.Duration(seconds) * time.Second}, nil
}

// Owner locks precede this settings lock. Retention decisions use the DB clock
// after both locks; an administrator cannot change policy during the transaction.
func avatarSourcePolicy(ctx context.Context, tx pgx.Tx) (AvatarBrowserPolicy, time.Time, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT document FROM shuttle_settings WHERE id=true FOR SHARE`).Scan(&raw); err != nil {
		return AvatarBrowserPolicy{}, time.Time{}, err
	}
	var settings Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return AvatarBrowserPolicy{}, time.Time{}, err
	}
	policy, err := settings.AvatarBrowserPolicy()
	if err != nil {
		return policy, time.Time{}, err
	}
	var now time.Time
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return policy, now.UTC(), err
}
