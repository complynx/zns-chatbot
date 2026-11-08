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

// UserRevokedError carries only text explicitly approved for the affected user.
type UserRevokedError struct{ PublicReason string }

func (e *UserRevokedError) Error() string { return ErrUserRevoked.Error() }
func (e *UserRevokedError) Unwrap() error { return ErrUserRevoked }

// UserBanResult identifies a committed local ban and its original audit receipt.
type UserBanResult struct {
	ID                uuid.UUID `json:"ban_id"`
	UserID            uuid.UUID `json:"user_id"`
	BotID             int64     `json:"bot_id"`
	TelegramUserID    int64     `json:"telegram_user_id"`
	Revoked           bool      `json:"revoked"`
	PreviouslyRevoked bool      `json:"previously_revoked"`
	CreatedAt         time.Time `json:"created_at"`
	PublicReason      string    `json:"public_reason,omitempty"`
}

// BanTelegramUser is an operator-only local database operation. It can block a
// known Telegram ID before onboarding; it never provisions or changes IAM.
// Exact actor/internal/public reasons form the replay key. No unban exists.
func (s *Store) BanTelegramUser(
	ctx context.Context,
	botID, telegramUserID int64,
	actor, reason, publicReason string,
) (UserBanResult, error) {
	if botID <= 0 || botID >= 1<<52 || telegramUserID <= 0 || telegramUserID >= 1<<52 ||
		strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" ||
		!utf8.ValidString(actor) || !utf8.ValidString(reason) ||
		utf8.RuneCountInString(actor) > 200 || utf8.RuneCountInString(reason) > 1000 || !validPublicBanReason(publicReason) {
		return UserBanResult{}, ErrUserBanInvalid
	}
	raw, _ := json.Marshal(struct {
		Actor        string `json:"actor"`
		Reason       string `json:"reason"`
		PublicReason string `json:"public_reason,omitempty"`
	}{actor, reason, publicReason})
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
	err = tx.QueryRow(ctx, `SELECT id, previously_revoked, created_at, public_reason FROM shuttle_user_bans
WHERE user_id=$1 AND request_hash=$2`, owner.ID, hash[:]).
		Scan(&result.ID, &result.PreviouslyRevoked, &result.CreatedAt, &result.PublicReason)
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
	_, err = tx.Exec(ctx, "UPDATE shuttle_users SET ban_public_reason=$2 WHERE id=$1", owner.ID, publicReason)
	if err != nil {
		return UserBanResult{}, err
	}
	result.ID, result.PreviouslyRevoked = uuid.New(), owner.Revoked
	result.PublicReason = publicReason
	err = tx.QueryRow(ctx, `INSERT INTO shuttle_user_bans
(id,user_id,actor,reason,request_hash,previously_revoked,public_reason) VALUES($1,$2,$3,$4,$5,$6,$7)
RETURNING created_at`, result.ID, owner.ID, actor, reason, hash[:], owner.Revoked, publicReason).Scan(&result.CreatedAt)
	if err != nil {
		return UserBanResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return UserBanResult{}, err
	}
	return result, nil
}

func validPublicBanReason(reason string) bool {
	return utf8.ValidString(reason) && utf8.RuneCountInString(reason) <= 1000 &&
		(reason == "" || strings.TrimSpace(reason) != "")
}

// CheckTelegramAccess reads only an existing local restriction before parsing a
// command. Intake repeats the check under its owner lock to cover concurrent bans.
func (s *Store) CheckTelegramAccess(ctx context.Context, botID, telegramUserID int64) error {
	var publicReason string
	err := s.pool.QueryRow(ctx, `SELECT ban_public_reason FROM shuttle_users
WHERE bot_id=$1 AND tg_user_id=$2 AND revoked`, botID, telegramUserID).Scan(&publicReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &UserRevokedError{PublicReason: publicReason}
}
