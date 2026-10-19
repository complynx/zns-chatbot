package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// AvatarDelivery tracks one document independently of a Loom task or native
// session. The Telegram recipient was captured when the artifact was admitted.
type AvatarDelivery struct {
	ID             uuid.UUID       `json:"id"`
	ArtifactID     uuid.UUID       `json:"artifact_id"`
	UserID         uuid.UUID       `json:"user_id"`
	TelegramUserID int64           `json:"telegram_user_id"`
	State          string          `json:"state"`
	Version        int64           `json:"version"`
	AttemptID      uuid.UUID       `json:"attempt_id"`
	Receipt        json.RawMessage `json:"receipt"`
	NextAttemptAt  time.Time       `json:"next_attempt_at"`
	CreatedAt      time.Time       `json:"created_at"`
}

func avatarDelivery(row sqlc.ShuttleAvatarDelivery) AvatarDelivery {
	return AvatarDelivery{
		ID:             row.ID,
		ArtifactID:     row.ArtifactID,
		UserID:         row.UserID,
		TelegramUserID: row.TelegramUserID,
		State:          row.State,
		Version:        row.Version,
		AttemptID:      uuid.UUID(row.AttemptID.Bytes),
		Receipt:        row.Receipt,
		NextAttemptAt:  row.NextAttemptAt.Time.UTC(),
		CreatedAt:      row.CreatedAt.Time.UTC(),
	}
}
func (s *Store) AvatarDelivery(ctx context.Context, scope EventScope, id uuid.UUID) (AvatarDelivery, error) {
	row, err := s.q.GetAvatarDelivery(
		ctx,
		sqlc.GetAvatarDeliveryParams{ID: id, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	return avatarDelivery(row), err
}

func (s *Store) ReadyAvatarDeliveries(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.q.ReadyAvatarDeliveries(
		ctx,
		sqlc.ReadyAvatarDeliveriesParams{
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
			AfterID:    after,
			PageLimit:  NotificationRepairPageSize,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]NotificationJob, 0, len(rows))
	for _, row := range rows {
		result = append(result, NotificationJob{ID: row.ID, Version: row.Version})
	}
	return result, nil
}
func (s *Store) AvatarDeliveryRecipient(ctx context.Context, scope EventScope, id uuid.UUID) (User, error) {
	row, err := s.q.AvatarDeliveryRecipient(
		ctx,
		sqlc.AvatarDeliveryRecipientParams{ID: id, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	return user(row), err
}

// ReserveAvatarDelivery requires fresh IAM verification before entry. Owner
// locking rechecks local revocation and captured recipient before the send fence.
func (s *Store) ReserveAvatarDelivery(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
	version int64,
	attempt uuid.UUID,
) (AvatarDelivery, []byte, error) {
	if attempt == uuid.Nil {
		return AvatarDelivery{}, nil, ErrInvalidAvatar
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarDelivery{}, nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, true); err != nil {
		return AvatarDelivery{}, nil, err
	}
	q := s.q.WithTx(tx)
	row, err := q.GetAvatarDelivery(
		ctx,
		sqlc.GetAvatarDeliveryParams{ID: id, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	if err != nil {
		return AvatarDelivery{}, nil, err
	}
	n := avatarDelivery(row)
	recipient, err := q.AvatarDeliveryRecipient(
		ctx,
		sqlc.AvatarDeliveryRecipientParams{ID: id, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	if err != nil {
		return n, nil, err
	}
	if recipient.ID != scope.UserID || recipient.ZitadelSubject != scope.Subject {
		return n, nil, pgx.ErrNoRows
	}
	content, err := avatarDeliveryContent(ctx, tx, n)
	if err != nil {
		return n, nil, err
	}
	reserved, err := q.ReserveAvatarDelivery(
		ctx,
		sqlc.ReserveAvatarDeliveryParams{
			ID:        id,
			Version:   version,
			AttemptID: pgtype.UUID{Bytes: attempt, Valid: true},
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, nil, ErrConflict
	}
	if err != nil {
		return n, nil, err
	}
	return avatarDelivery(reserved), content, tx.Commit(ctx)
}
func avatarDeliveryContent(ctx context.Context, tx pgx.Tx, n AvatarDelivery) ([]byte, error) {
	var raw, content []byte
	err := tx.QueryRow(ctx, `SELECT receipt,jpeg FROM shuttle_avatars WHERE id=$1 AND user_id=$2 AND draft_id IS NOT NULL`, n.ArtifactID, n.UserID).
		Scan(&raw, &content)
	if err != nil {
		return nil, err
	}
	var receipt AvatarDraftReceipt
	if json.Unmarshal(raw, &receipt) != nil {
		return nil, ErrInvalidAvatar
	}
	digest := sha256.Sum256(content)
	if receipt.ArtifactID != n.ArtifactID || receipt.DeliveryID != n.ID || receipt.SizeBytes != int64(len(content)) ||
		len(content) == 0 ||
		len(content) > avatar.MaxOutputBytes ||
		receipt.SHA256 != hex.EncodeToString(digest[:]) ||
		!bytes.HasPrefix(content, []byte{0xff, 0xd8}) {
		return nil, ErrInvalidAvatar
	}
	return content, nil
}
func (s *Store) BlockAvatarDelivery(ctx context.Context, scope EventScope, id uuid.UUID, version int64) error {
	if _, err := s.AvatarDelivery(ctx, scope, id); err != nil {
		return err
	}
	changed, err := s.q.BlockAvatarDelivery(ctx, sqlc.BlockAvatarDeliveryParams{ID: id, Version: version})
	if err == nil && changed != 1 {
		return ErrConflict
	}
	return err
}

// FinishAvatarDelivery accepts only the exact reserved attempt and version.
// Unknown outcomes stay terminal until an explicit authorized repair.
func (s *Store) FinishAvatarDelivery(
	ctx context.Context,
	n AvatarDelivery,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	if !notificationOutcomeValid(state) || n.AttemptID == uuid.Nil || n.Version < 1 || delay < 0 ||
		delay > 24*time.Hour {
		return errors.New("invalid avatar delivery outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	changed, err := s.q.FinishAvatarDelivery(
		ctx,
		sqlc.FinishAvatarDeliveryParams{
			ID:              n.ID,
			ExpectedVersion: n.Version,
			AttemptID:       pgtype.UUID{Bytes: n.AttemptID, Valid: true},
			Outcome:         state,
			Receipt:         receipt,
			DelayMs:         delay.Milliseconds(),
		},
	)
	if err == nil && changed != 1 {
		return ErrConflict
	}
	return err
}

// RecoverAvatarDeliveries requires the application's exclusive instance lock,
// before any workers start. It never authorizes an automatic resend.
func (s *Store) RecoverAvatarDeliveries(ctx context.Context, scope EventScope) error {
	_, err := s.q.RecoverAvatarDeliveries(
		ctx,
		sqlc.RecoverAvatarDeliveriesParams{BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	return err
}
