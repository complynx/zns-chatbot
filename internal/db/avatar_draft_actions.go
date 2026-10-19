package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type NativeAvatarDraftAction struct {
	OperationID uuid.UUID  `json:"operation_id"`
	DraftID     uuid.UUID  `json:"draft_id"`
	ArtifactID  *uuid.UUID `json:"artifact_id,omitempty"`
	DeliveryID  *uuid.UUID `json:"delivery_id,omitempty"`
	Kind        string     `json:"kind"`
	Version     int64      `json:"version"`
	CompletedAt time.Time  `json:"completed_at"`
}

// RecentNativeAvatarDraftActions gives the reactive agent bounded historical
// metadata. It never starts a task or exposes original image bytes.
func (s *Store) RecentNativeAvatarDraftActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]NativeAvatarDraftAction, error) {
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
		`SELECT actions.operation_id,actions.draft_id,actions.kind,actions.version,actions.created_at,actions.artifact_id,actions.delivery_id
 FROM (
 SELECT user_id,operation_id,draft_id,kind,(result->>'version')::bigint AS version,created_at,NULL::uuid AS artifact_id,NULL::uuid AS delivery_id FROM shuttle_avatar_draft_operations WHERE user_id=$2 AND native
 UNION ALL
 SELECT user_id,operation_id,draft_id,'finalize',draft_version,created_at,id, (receipt->>'delivery_id')::uuid FROM shuttle_avatars WHERE user_id=$2 AND native AND draft_id IS NOT NULL
 ) actions JOIN shuttle_tasks task ON task.user_id=actions.user_id
 WHERE task.id=$1 AND task.user_id=$2 AND actions.created_at<task.created_at
 ORDER BY actions.created_at DESC,actions.operation_id LIMIT 5`,
		taskID,
		scope.UserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]NativeAvatarDraftAction, 0)
	for rows.Next() {
		var a NativeAvatarDraftAction
		var artifact, delivery pgtype.UUID
		if err = rows.Scan(
			&a.OperationID,
			&a.DraftID,
			&a.Kind,
			&a.Version,
			&a.CompletedAt,
			&artifact,
			&delivery,
		); err != nil {
			return nil, err
		}
		if artifact.Valid {
			id := uuid.UUID(artifact.Bytes)
			a.ArtifactID = &id
		}
		if delivery.Valid {
			id := uuid.UUID(delivery.Bytes)
			a.DeliveryID = &id
		}
		a.CompletedAt = a.CompletedAt.UTC()
		result = append(result, a)
	}
	return result, rows.Err()
}
