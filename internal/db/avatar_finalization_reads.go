package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

// AvatarDraftFinalize snapshots a saved draft. Its transport and Loom run are
// deliberately absent from the operation identity.
type AvatarDraftFinalize struct {
	OperationID     uuid.UUID `json:"operation_id"`
	DraftID         uuid.UUID `json:"draft_id"`
	ExpectedVersion int64     `json:"expected_version"`
}

// AvatarDraftReceipt proves persistence and admission to delivery, not Telegram
// delivery. It is distinct from the unchanged legacy task-based receipt.
type AvatarDraftReceipt struct {
	ArtifactID       uuid.UUID        `json:"artifact_id"`
	DeliveryID       uuid.UUID        `json:"delivery_id"`
	DraftID          uuid.UUID        `json:"draft_id"`
	DraftVersion     int64            `json:"draft_version"`
	SourceSHA256     string           `json:"source_sha256"`
	FrameSHA256      string           `json:"frame_sha256"`
	ProcessorVersion string           `json:"processor_version"`
	Transform        avatar.Transform `json:"transform"`
	SHA256           string           `json:"sha256"`
	Width            int              `json:"width"`
	Height           int              `json:"height"`
	SizeBytes        int64            `json:"size_bytes"`
	CreatedAt        time.Time        `json:"created_at"`
}

func avatarFinalizeHash(a AvatarDraftFinalize) ([]byte, error) {
	if a.OperationID == uuid.Nil || a.DraftID == uuid.Nil || a.ExpectedVersion < 1 {
		return nil, ErrInvalidAvatar
	}
	// Kind makes the artifact operation namespace explicit across both origins.
	raw, err := json.Marshal(struct {
		Kind    string              `json:"kind"`
		Request AvatarDraftFinalize `json:"request"`
	}{"finalize_draft", a})
	digest := sha256.Sum256(raw)
	return digest[:], err
}

func avatarFinalizationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	owner, operation uuid.UUID,
	hash []byte,
) (*AvatarDraftReceipt, error) {
	var stored, raw []byte
	var draft pgtype.UUID
	err := tx.QueryRow(ctx, "SELECT request_hash,receipt,draft_id FROM shuttle_avatars WHERE user_id=" +
"$1 AND operation_id=$2", owner, operation).
		Scan(&stored, &raw, &draft)
	if err != nil {
		return nil, err
	}
	if hash != nil && (!draft.Valid || !bytes.Equal(hash, stored)) {
		return nil, ErrConflict
	}
	if !draft.Valid {
		return nil, pgx.ErrNoRows
	}
	var result AvatarDraftReceipt
	err = json.Unmarshal(raw, &result)
	return &result, err
}

// AvatarDraftFinalizePreflight reconciles committed operations before any source
// read/render. A nil receipt means the current owned draft may be rendered.
func (s *Store) AvatarDraftFinalizePreflight(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftFinalize,
) (*AvatarDraftReceipt, error) {
	hash, err := avatarFinalizeHash(a)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	prior, err := avatarFinalizationReceipt(ctx, tx, scope.UserID, a.OperationID, hash)
	if !errors.Is(err, pgx.ErrNoRows) {
		return prior, err
	}
	d, _, err := readAvatarDraft(ctx, tx, scope.UserID, a.DraftID, false)
	if err == nil && d.SourceExpired {
		return nil, ErrAvatarSourceExpired
	}
	if err == nil && (d.Version != a.ExpectedVersion || d.FrameSHA256 != avatar.FrameSHA256) {
		err = ErrConflict
	}
	return nil, err
}

func (s *Store) AvatarDraftFinalizationOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (AvatarDraftReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarDraftReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarDraftReceipt{}, err
	}
	result, err := avatarFinalizationReceipt(ctx, tx, scope.UserID, operation, nil)
	if err != nil {
		return AvatarDraftReceipt{}, err
	}
	return *result, nil
}

func (s *Store) GetFinalizedAvatar(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
) (AvatarDraftReceipt, []byte, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarDraftReceipt{}, nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarDraftReceipt{}, nil, err
	}
	var raw, jpeg []byte
	err = tx.QueryRow(ctx, "SELECT receipt,jpeg FROM shuttle_avatars WHERE id=$1 AND user_id=$2 AND " +
"draft_id IS NOT NULL", id, scope.UserID).
		Scan(&raw, &jpeg)
	if err != nil {
		return AvatarDraftReceipt{}, nil, err
	}
	var result AvatarDraftReceipt
	err = json.Unmarshal(raw, &result)
	return result, jpeg, err
}
