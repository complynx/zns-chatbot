package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/miniapp"
)

const (
	miniAppPending = "pending"
	miniAppActive  = "active"
	miniAppRevoked = "revoked"
)

var ErrMiniAppDenied = errors.New("mini app session denied")

// MiniAppClaim must come from verified Telegram initData, never client user IDs.
type MiniAppClaim struct {
	BotID, TelegramUserID            int64
	Organization                     string
	Profile                          json.RawMessage
	ProofHash, NonceHash, Generation [sha256.Size]byte
	AuthDate                         time.Time
	MaxAge, PendingTTL, SessionTTL   time.Duration
}

type MiniAppAttempt struct {
	ID        uuid.UUID
	User      User
	State     string
	ExpiresAt time.Time
}

type MiniAppActivation struct {
	ID                                uuid.UUID
	BotID                             int64
	NonceHash, Generation, BearerHash [sha256.Size]byte
	Subject, Organization             string
}

func miniAppAttempt(row sqlc.ShuttleMiniappSession, owner sqlc.ShuttleUser) MiniAppAttempt {
	return MiniAppAttempt{ID: row.ID, User: user(owner), State: row.State, ExpiresAt: row.ExpiresAt.Time}
}

// ClaimMiniAppSession persists the proposed identity UUID before any IAM call.
// It preserves existing Telegram metadata and never creates a conversation/job.
func (s *Store) ClaimMiniAppSession(ctx context.Context, claim MiniAppClaim) (MiniAppAttempt, error) {
	if claim.BotID <= 0 || claim.TelegramUserID <= 0 || claim.Organization == "" || !json.Valid(claim.Profile) ||
		claim.MaxAge <= 0 || claim.MaxAge > 24*time.Hour || claim.PendingTTL <= 0 || claim.PendingTTL > 5*time.Minute ||
		claim.SessionTTL < time.Minute || claim.SessionTTL > time.Hour {
		return MiniAppAttempt{}, ErrMiniAppDenied
	}
	// Prune outside the user/session transaction to preserve its lock order.
	// Retention covers the verifier's maximum possible freshness, not today's setting.
	if _, err := s.q.PruneMiniAppSessions(ctx, claim.BotID); err != nil {
		return MiniAppAttempt{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := q.EnsureTelegramUser(ctx, sqlc.EnsureTelegramUserParams{
		ID: uuid.New(), BotID: claim.BotID, TgUserID: claim.TelegramUserID, Profile: claim.Profile})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	if owner.Revoked || (owner.ZitadelOrg != "" && owner.ZitadelOrg != claim.Organization) {
		return MiniAppAttempt{}, ErrMiniAppDenied
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	pending := minTime(now.Time.Add(claim.PendingTTL), claim.AuthDate.Add(claim.MaxAge))
	err = q.InsertMiniAppSession(ctx, sqlc.InsertMiniAppSessionParams{
		ID: uuid.New(), BotID: claim.BotID, UserID: owner.ID, ProofHash: claim.ProofHash[:],
		NonceHash: claim.NonceHash[:], KeyGeneration: claim.Generation[:], Subject: owner.ZitadelSubject,
		Organization: claim.Organization, AuthDate: pgtype.Timestamptz{Time: claim.AuthDate, Valid: true},
		PendingUntil: pgtype.Timestamptz{Time: pending, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: now.Time.Add(claim.SessionTTL), Valid: true}})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	row, err := q.LockMiniAppProof(ctx, sqlc.LockMiniAppProofParams{BotID: claim.BotID, ProofHash: claim.ProofHash[:]})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	if row.UserID != owner.ID || row.Organization != claim.Organization || !row.AuthDate.Time.Equal(claim.AuthDate) ||
		!hmac.Equal(row.NonceHash, claim.NonceHash[:]) {
		return MiniAppAttempt{}, ErrConflict
	}
	now, err = q.RegistrationNow(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	if !miniAppCurrent(row, owner, claim.Generation, now.Time) ||
		!claim.AuthDate.Add(claim.MaxAge).
			After(now.Time) ||
		claim.AuthDate.After(now.Time.Add(miniapp.LoginFutureSkew)) {
		return MiniAppAttempt{}, ErrMiniAppDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return MiniAppAttempt{}, err
	}
	return miniAppAttempt(row, owner), nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// miniAppCurrent runs only after locking the current owner then the session.
func miniAppCurrent(
	row sqlc.ShuttleMiniappSession,
	owner sqlc.ShuttleUser,
	generation [sha256.Size]byte,
	now time.Time,
) bool {
	if row.UserID != owner.ID || row.BotID != owner.BotID || owner.Revoked || row.State == miniAppRevoked ||
		!row.ExpiresAt.Time.After(now) || !hmac.Equal(row.KeyGeneration, generation[:]) {
		return false
	}
	if row.State == miniAppActive {
		return row.Subject != "" && row.Subject == owner.ZitadelSubject && row.Organization == owner.ZitadelOrg
	}
	return row.State == miniAppPending && row.PendingUntil.Time.After(now) &&
		(row.Subject == "" || row.Subject == owner.ZitadelSubject) &&
		(owner.ZitadelOrg == "" || row.Organization == owner.ZitadelOrg)
}

// ActivateMiniAppSession follows IAM verification. It rechecks identity while
// locking USER then SESSION, matching Claim and avoiding network calls under locks.
func (s *Store) ActivateMiniAppSession(ctx context.Context, activation MiniAppActivation) (MiniAppAttempt, error) {
	if activation.Subject == "" || activation.Organization == "" {
		return MiniAppAttempt{}, ErrMiniAppDenied
	}
	initial, err := s.q.GetMiniAppSession(ctx, activation.ID)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := q.LockTelegramUser(ctx, sqlc.LockTelegramUserParams{ID: initial.UserID, BotID: activation.BotID})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	row, err := q.LockMiniAppSession(ctx, activation.ID)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	if !miniAppCurrent(row, owner, activation.Generation, now.Time) || row.Organization != activation.Organization ||
		!hmac.Equal(row.NonceHash, activation.NonceHash[:]) ||
		(owner.ZitadelSubject != "" && owner.ZitadelSubject != activation.Subject) {
		return MiniAppAttempt{}, ErrMiniAppDenied
	}
	if row.State == miniAppActive && !hmac.Equal(row.BearerHash, activation.BearerHash[:]) {
		return MiniAppAttempt{}, ErrConflict
	}
	owner, err = q.BindIdentity(ctx, sqlc.BindIdentityParams{
		ID: owner.ID, ZitadelSubject: activation.Subject, ZitadelOrg: activation.Organization})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	row, err = q.ActivateMiniAppSession(ctx, sqlc.ActivateMiniAppSessionParams{
		ID:           row.ID,
		Subject:      activation.Subject,
		Organization: activation.Organization,
		BearerHash:   activation.BearerHash[:],
	})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MiniAppAttempt{}, err
	}
	return miniAppAttempt(row, owner), nil
}

func (s *Store) MiniAppSession(
	ctx context.Context,
	botID int64,
	bearer, generation [sha256.Size]byte,
) (MiniAppAttempt, error) {
	return s.miniAppBearer(ctx, botID, bearer, generation, false)
}

func (s *Store) LogoutMiniAppSession(ctx context.Context, botID int64, bearer, generation [sha256.Size]byte) error {
	_, err := s.miniAppBearer(ctx, botID, bearer, generation, true)
	return err
}

func (s *Store) miniAppBearer(
	ctx context.Context,
	botID int64,
	bearer, generation [sha256.Size]byte,
	revoke bool,
) (MiniAppAttempt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	attempt, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return MiniAppAttempt{}, err
	}

	if revoke {
		if err = s.q.WithTx(tx).RevokeMiniAppSession(ctx, attempt.ID); err != nil {
			return MiniAppAttempt{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return MiniAppAttempt{}, err
		}
		attempt.State = miniAppRevoked
	}
	return attempt, nil
}

// lockMiniAppBearer holds the owner and session locks until the caller finishes
// its transaction, so logout and local revocation serialize with business writes.
func (s *Store) lockMiniAppBearer(
	ctx context.Context, tx pgx.Tx, botID int64, bearer, generation [sha256.Size]byte,
) (MiniAppAttempt, error) {
	q := s.q.WithTx(tx)
	initial, err := q.FindMiniAppBearer(ctx, sqlc.FindMiniAppBearerParams{BotID: botID, BearerHash: bearer[:]})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	owner, err := q.LockTelegramUser(ctx, sqlc.LockTelegramUserParams{ID: initial.UserID, BotID: botID})
	if err != nil {
		return MiniAppAttempt{}, err
	}
	row, err := q.LockMiniAppSession(ctx, initial.ID)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return MiniAppAttempt{}, err
	}
	if row.State != miniAppActive || !hmac.Equal(row.BearerHash, bearer[:]) ||
		!miniAppCurrent(row, owner, generation, now.Time) {
		return MiniAppAttempt{}, ErrMiniAppDenied
	}
	return miniAppAttempt(row, owner), nil
}

// MutateMiniAppProfile validates the current session in the same transaction as
// the profile and operation receipt. IAM must be checked before calling it.
func (s *Store) MutateMiniAppProfile(
	ctx context.Context, botID int64, bearer, generation [sha256.Size]byte, mutation ProfileMutation,
) (UserProfile, error) {
	if mutation.Kind != profileSetName && mutation.Kind != profileSetLanguage {
		return UserProfile{}, ErrInvalidProfile
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserProfile{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	attempt, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return UserProfile{}, err
	}
	p, err := mutateProfile(ctx, tx, attempt.User.ID, mutation)
	if err != nil {
		return p, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_miniapp_profile_actions(user_id,operation_id,kind)
VALUES($1,$2,$3) ON CONFLICT(user_id,operation_id) DO NOTHING`, attempt.User.ID, mutation.OperationID, mutation.Kind)
	if err != nil {
		return UserProfile{}, err
	}
	return p, tx.Commit(ctx)
}
