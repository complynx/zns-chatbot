package db

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type AvatarCropOpen struct {
	DraftID  uuid.UUID `json:"draft_id"`
	OpenedAt time.Time `json:"opened_at"`
}

// RecordMiniAppAvatarCropOpen records first deliberate entry, never image GETs.
// The mutable delivery UI namespace is separate from the immutable artifact receipt.
func (s *Store) RecordMiniAppAvatarCropOpen(
	ctx context.Context, scope ProfileScope, bearer, generation [sha256.Size]byte, draftID, operation uuid.UUID,
) (AvatarCropOpen, error) {
	result := AvatarCropOpen{DraftID: draftID}
	if draftID == uuid.Nil || operation == uuid.Nil {
		return result, ErrInvalidAvatar
	}
	tx, err := s.avatarDraftTransaction(ctx, scope, &avatarDraftSession{bearer, generation})
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	var deliveryID uuid.UUID
	var opened pgtype.Timestamptz
	err = tx.QueryRow(ctx, `SELECT n.id,(n.receipt#>>'{ui,crop_opened_at}')::timestamptz
FROM shuttle_avatars a JOIN shuttle_avatar_deliveries n ON n.artifact_id=a.id AND n.user_id=a.user_id
WHERE a.user_id=$1 AND a.draft_id=$2
ORDER BY a.draft_version,a.created_at,a.id LIMIT 1 FOR UPDATE OF n`, scope.UserID, draftID).
		Scan(&deliveryID, &opened)
	if err != nil {
		return result, err
	}
	if opened.Valid {
		result.OpenedAt = opened.Time.UTC()
		return result, nil
	}
	err = tx.QueryRow(ctx, `UPDATE shuttle_avatar_deliveries
SET receipt=receipt || jsonb_build_object('ui',
 (CASE WHEN jsonb_typeof(receipt->'ui')='object' THEN receipt->'ui' ELSE '{}'::jsonb END) ||
 jsonb_build_object('crop_opened_at',clock_timestamp(),'crop_open_operation_id',$2::text))
WHERE id=$1 RETURNING (receipt#>>'{ui,crop_opened_at}')::timestamptz`, deliveryID, operation.String()).
		Scan(&result.OpenedAt)
	if err != nil {
		return result, err
	}
	result.OpenedAt = result.OpenedAt.UTC()
	return result, tx.Commit(ctx)
}
