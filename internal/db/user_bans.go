package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

var (
	ErrUserRevoked    = errors.New("local user access revoked")
	ErrUserBanInvalid = errors.New("invalid Telegram user ban")
)

// UserBanResult identifies a committed local ban and its original audit receipt.
type UserBanResult struct {
	ID                uuid.UUID `json:"ban_id"`
	UserID            uuid.UUID `json:"user_id"`
	BotID             int64     `json:"bot_id"`
	TelegramUserID    int64     `json:"telegram_user_id"`
	Revoked           bool      `json:"revoked"`
	PreviouslyRevoked bool      `json:"previously_revoked"`
	CreatedAt         time.Time `json:"created_at"`
}

// BanTelegramUser is an operator-only local database operation. It can block a
// known Telegram ID before onboarding; it never provisions or changes IAM.
// The exact actor/reason pair is the replay key for this owner. No unban exists.
func (s *Store) BanTelegramUser(
	ctx context.Context,
	botID, telegramUserID int64,
	actor, reason string,
) (UserBanResult, error) {
	if botID <= 0 || botID >= 1<<52 || telegramUserID <= 0 || telegramUserID >= 1<<52 ||
		strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" ||
		!utf8.ValidString(actor) || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(actor) > 200 || utf8.RuneCountInString(reason) > 1000 {
		return UserBanResult{}, ErrUserBanInvalid
	}
	raw, _ := json.Marshal(struct {
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}{actor, reason})
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserBanResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	// The upsert holds this owner's row lock through the ban/audit commit and
	// serializes with Intake and MiniApp claims using the same owner key.
	owner, err := q.EnsureTelegramUser(ctx, sqlc.EnsureTelegramUserParams{
		ID: uuid.New(), BotID: botID, TgUserID: telegramUserID, Profile: json.RawMessage(`{}`),
	})
	if err != nil {
		return UserBanResult{}, err
	}
	result := UserBanResult{UserID: owner.ID, BotID: botID, TelegramUserID: telegramUserID, Revoked: true}
	err = tx.QueryRow(ctx, `SELECT id, previously_revoked, created_at FROM shuttle_user_bans
WHERE user_id=$1 AND request_hash=$2`, owner.ID, hash[:]).Scan(&result.ID, &result.PreviouslyRevoked, &result.CreatedAt)
	if err == nil {
		if !owner.Revoked {
			return UserBanResult{}, ErrConflict
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UserBanResult{}, err
	}
	if _, err = revokeUserTx(ctx, q, owner.ID, actor); err != nil {
		return UserBanResult{}, err
	}
	result.ID, result.PreviouslyRevoked = uuid.New(), owner.Revoked
	err = tx.QueryRow(ctx, `INSERT INTO shuttle_user_bans
(id,user_id,actor,reason,request_hash,previously_revoked) VALUES($1,$2,$3,$4,$5,$6)
RETURNING created_at`, result.ID, owner.ID, actor, reason, hash[:], owner.Revoked).Scan(&result.CreatedAt)
	if err != nil {
		return UserBanResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return UserBanResult{}, err
	}
	return result, nil
}
