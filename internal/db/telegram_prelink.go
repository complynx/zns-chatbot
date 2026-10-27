package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

var ErrTelegramPrelinkDenied = errors.New("telegram prelink denied")

const (
	prelinkCompleted    = "completed"
	prelinkMaxRemaining = time.Minute + 5*time.Second
	prelinkFieldLimit   = 200
	prelinkIssuerLimit  = 2048
)

// TelegramPrelinkClaim comes only from a freshly verified provider assertion.
// Pin bot/org/provider on the server; never accept an IAM target from the browser.
// AssertionHash is SHA-256 of the exact compact proof, not a reserialized payload.
type TelegramPrelinkClaim struct {
	BotID, TelegramUserID                                                   int64
	Organization, ProviderIssuer, ProviderID, AssertionJTI, ExternalSubject string
	Profile                                                                 json.RawMessage
	AssertionHash                                                           [sha256.Size]byte
	ExpiresAt                                                               time.Time
}

type TelegramPrelinkAttempt struct {
	ID             uuid.UUID
	User           User
	State, Subject string
	ExpiresAt      time.Time
}

func (claim TelegramPrelinkClaim) valid() bool {
	return claim.BotID > 0 && claim.BotID < 1<<52 && claim.TelegramUserID > 0 && claim.TelegramUserID < 1<<52 &&
		prelinkText(claim.Organization, prelinkFieldLimit) && prelinkText(claim.ProviderIssuer, prelinkIssuerLimit) &&
		prelinkText(claim.ProviderID, prelinkFieldLimit) && prelinkText(claim.AssertionJTI, prelinkFieldLimit) &&
		prelinkText(claim.ExternalSubject, prelinkFieldLimit) && json.Valid(claim.Profile) &&
		!claim.ExpiresAt.IsZero() && claim.ExpiresAt.Nanosecond() == 0
}

func prelinkText(value string, limit int) bool {
	return value != "" && len(value) <= limit &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func prelinkAttempt(row sqlc.ShuttleTelegramPrelink, owner sqlc.ShuttleUser) TelegramPrelinkAttempt {
	return TelegramPrelinkAttempt{
		ID:        row.ID,
		User:      user(owner),
		State:     row.State,
		Subject:   row.Subject,
		ExpiresAt: row.ExpiresAt.Time,
	}
}

// ClaimTelegramPrelink reserves durable intent and the same canonical user used
// by bot/Mini App. It commits before any IAM call and creates no conversation/job.
// Completed receipts still require fresh IAM/link authorization before relay.
func (s *Store) ClaimTelegramPrelink(ctx context.Context, claim TelegramPrelinkClaim) (TelegramPrelinkAttempt, error) {
	if !claim.valid() {
		return TelegramPrelinkAttempt{}, ErrTelegramPrelinkDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := q.EnsureTelegramUser(
		ctx,
		sqlc.EnsureTelegramUserParams{
			ID:       uuid.New(),
			BotID:    claim.BotID,
			TgUserID: claim.TelegramUserID,
			Profile:  claim.Profile,
		},
	)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	if !prelinkOwner(owner, claim) || !claim.ExpiresAt.After(now.Time) ||
		claim.ExpiresAt.After(now.Time.Add(prelinkMaxRemaining)) {
		return TelegramPrelinkAttempt{}, ErrTelegramPrelinkDenied
	}
	err = q.InsertTelegramPrelink(
		ctx,
		sqlc.InsertTelegramPrelinkParams{
			ID:              uuid.New(),
			BotID:           claim.BotID,
			UserID:          owner.ID,
			ProviderIssuer:  claim.ProviderIssuer,
			AssertionJti:    claim.AssertionJTI,
			AssertionHash:   claim.AssertionHash[:],
			Organization:    claim.Organization,
			ProviderID:      claim.ProviderID,
			ExternalSubject: claim.ExternalSubject,
			ExpiresAt:       pgtype.Timestamptz{Time: claim.ExpiresAt, Valid: true},
		},
	)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	row, err := q.LockTelegramPrelinkProof(
		ctx,
		sqlc.LockTelegramPrelinkProofParams{ProviderIssuer: claim.ProviderIssuer, AssertionJti: claim.AssertionJTI},
	)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	if !prelinkMatches(row, owner, claim) {
		return TelegramPrelinkAttempt{}, ErrConflict
	}
	now, err = q.RegistrationNow(ctx)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	if !row.ExpiresAt.Time.After(now.Time) {
		return TelegramPrelinkAttempt{}, ErrTelegramPrelinkDenied
	}
	if row.State == prelinkCompleted && (owner.ZitadelSubject != row.Subject || owner.ZitadelOrg != row.Organization) {
		return TelegramPrelinkAttempt{}, ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	return prelinkAttempt(row, owner), nil
}

func prelinkOwner(owner sqlc.ShuttleUser, claim TelegramPrelinkClaim) bool {
	return owner.BotID == claim.BotID && owner.TgUserID == claim.TelegramUserID && !owner.Revoked &&
		(owner.ZitadelOrg == "" || owner.ZitadelOrg == claim.Organization) &&
		(owner.ZitadelSubject == "" || owner.ZitadelOrg == claim.Organization)
}

func prelinkMatches(row sqlc.ShuttleTelegramPrelink, owner sqlc.ShuttleUser, claim TelegramPrelinkClaim) bool {
	return row.UserID == owner.ID &&
		row.BotID == claim.BotID &&
		row.Organization == claim.Organization &&
		row.ProviderIssuer == claim.ProviderIssuer &&
		row.ProviderID == claim.ProviderID &&
		row.ExternalSubject == claim.ExternalSubject &&
		row.AssertionJti == claim.AssertionJTI &&
		row.ExpiresAt.Time.Equal(claim.ExpiresAt) &&
		hmac.Equal(row.AssertionHash, claim.AssertionHash[:])
}

// CompleteTelegramPrelink follows fresh IAM account/link verification. It locks
// USER then RECEIPT, rechecks proof and current authority, and binds/completes in
// one transaction. An expired or revoked completed receipt cannot be replayed.
func (s *Store) CompleteTelegramPrelink(
	ctx context.Context,
	id uuid.UUID,
	claim TelegramPrelinkClaim,
	subject string,
) (TelegramPrelinkAttempt, error) {
	if id == uuid.Nil || !claim.valid() || !prelinkText(subject, prelinkFieldLimit) {
		return TelegramPrelinkAttempt{}, ErrTelegramPrelinkDenied
	}
	initial, err := s.q.GetTelegramPrelink(ctx, id)
	if err != nil {
		return TelegramPrelinkAttempt{}, prelinkDBError(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := q.LockTelegramUser(ctx, sqlc.LockTelegramUserParams{ID: initial.UserID, BotID: claim.BotID})
	if err != nil {
		return TelegramPrelinkAttempt{}, prelinkDBError(err)
	}
	row, err := q.LockTelegramPrelink(ctx, id)
	if err != nil {
		return TelegramPrelinkAttempt{}, prelinkDBError(err)
	}
	if !prelinkMatches(row, owner, claim) {
		return TelegramPrelinkAttempt{}, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	if !prelinkOwner(owner, claim) || !row.ExpiresAt.Time.After(now.Time) {
		return TelegramPrelinkAttempt{}, ErrTelegramPrelinkDenied
	}
	if owner.ZitadelSubject != "" && owner.ZitadelSubject != subject {
		return TelegramPrelinkAttempt{}, ErrConflict
	}
	if row.State == prelinkCompleted {
		if row.Subject != subject || owner.ZitadelSubject != subject || owner.ZitadelOrg != claim.Organization {
			return TelegramPrelinkAttempt{}, ErrConflict
		}
	} else {
		owner, err = q.BindIdentity(
			ctx,
			sqlc.BindIdentityParams{ID: owner.ID, ZitadelSubject: subject, ZitadelOrg: claim.Organization},
		)
		if err != nil {
			return TelegramPrelinkAttempt{}, prelinkDBError(err)
		}
		row, err = q.CompleteTelegramPrelink(ctx, sqlc.CompleteTelegramPrelinkParams{ID: id, Subject: subject})
		if err != nil {
			return TelegramPrelinkAttempt{}, prelinkDBError(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return TelegramPrelinkAttempt{}, err
	}
	return prelinkAttempt(row, owner), nil
}

func prelinkDBError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTelegramPrelinkDenied
	}
	var violation *pgconn.PgError
	if errors.As(err, &violation) && violation.Code == "23505" {
		return ErrConflict
	}
	return err
}
