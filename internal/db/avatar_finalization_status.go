package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// AvatarFinalizationView separates immutable admission from current delivery.
// Transport attempts and operational receipts remain administrator-only.
type AvatarFinalizationView struct {
	Avatar          AvatarDraftReceipt `json:"avatar"`
	DeliveryState   string             `json:"delivery_state"`
	DeliveryVersion int64              `json:"delivery_version"`
}

func (s *Store) FinalizedAvatarStatus(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
) (AvatarFinalizationView, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarFinalizationView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarFinalizationView{}, err
	}
	var raw []byte
	var delivery uuid.UUID
	var result AvatarFinalizationView
	err = tx.QueryRow(ctx, "SELECT a.receipt,n.id,n.state,n.version FROM shuttle_avatars a JOIN "+
		"shuttle_avatar_deliveries n ON n.artifact_id=a.id AND n.user_id=a.user_id WHERE "+
		"a.id=$1 AND a.user_id=$2 AND a.draft_id IS NOT NULL", id, scope.UserID).
		Scan(&raw, &delivery, &result.DeliveryState, &result.DeliveryVersion)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result.Avatar); err != nil {
		return result, err
	}
	if result.Avatar.ArtifactID != id || result.Avatar.DeliveryID != delivery {
		return AvatarFinalizationView{}, ErrInvalidAvatar
	}
	return result, nil
}

// ListFinalizedAvatars lets an owner recover artifact locators on another device.
// The shared artifact quota bounds this metadata-only read to twenty entries.
func (s *Store) ListFinalizedAvatars(ctx context.Context, scope ProfileScope) ([]AvatarFinalizationView, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(
		ctx,
		"SELECT a.id,a.receipt,n.id,n.state,n.version FROM shuttle_avatars a JOIN "+
			"shuttle_avatar_deliveries n ON n.artifact_id=a.id AND n.user_id=a.user_id WHERE "+
			"a.user_id=$1 AND a.draft_id IS NOT NULL ORDER BY a.created_at DESC,a.id DESC LIMIT $2",
		scope.UserID,
		MaxAvatarsPerOwner,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AvatarFinalizationView, 0)
	for rows.Next() {
		var raw []byte
		var id, delivery uuid.UUID
		var view AvatarFinalizationView
		if err = rows.Scan(&id, &raw, &delivery, &view.DeliveryState, &view.DeliveryVersion); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &view.Avatar); err != nil {
			return nil, err
		}
		if view.Avatar.ArtifactID != id || view.Avatar.DeliveryID != delivery {
			return nil, ErrInvalidAvatar
		}
		result = append(result, view)
	}
	return result, rows.Err()
}
