package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type NativeAvatarDraftAction struct {
	OperationID       uuid.UUID  `json:"operation_id"`
	DraftID           uuid.UUID  `json:"draft_id"`
	ArtifactID        *uuid.UUID `json:"artifact_id,omitempty"`
	DeliveryID        *uuid.UUID `json:"delivery_id,omitempty"`
	SourceFileID      *uuid.UUID `json:"source_file_id,omitempty"`
	SourceKind        string     `json:"source_kind,omitempty"`
	DeliveryState     string     `json:"delivery_state,omitempty"`
	CropButtonOffered bool       `json:"crop_button_offered"`
	CropOpenedAt      *time.Time `json:"crop_opened_at,omitempty"`
	Kind              string     `json:"kind"`
	Version           int64      `json:"version"`
	CompletedAt       time.Time  `json:"completed_at"`
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
		"SELECT actions.operation_id,actions.draft_id,actions.kind,actions.version,"+
			"actions.created_at,actions.artifact_id,actions.delivery_id,\n"+
			" d.source_file_id,coalesce(f.kind,''),coalesce(n.state,''),"+
			"coalesce((n.receipt->>'crop_button_offered')::boolean,false),\n"+
			" (n.receipt#>>'{ui,crop_opened_at}')::timestamptz\n"+
			" FROM (\n"+
			" SELECT user_id,operation_id,draft_id,kind,(result->>'version')::bigint AS version,"+
			"created_at,NULL::uuid AS artifact_id,NULL::uuid AS delivery_id FROM "+
			"shuttle_avatar_draft_operations WHERE user_id=$2 AND native\n"+
			" UNION ALL\n"+
			" SELECT user_id,operation_id,draft_id,'finalize',draft_version,created_at,id, "+
			"(receipt->>'delivery_id')::uuid FROM shuttle_avatars WHERE user_id=$2 AND native AND "+
			"draft_id IS NOT NULL\n"+
			" UNION ALL\n"+
			" SELECT a.user_id,(n.receipt#>>'{ui,crop_open_operation_id}')::uuid,a.draft_id,"+
			"'crop_open',a.draft_version,\n"+
			" (n.receipt#>>'{ui,crop_opened_at}')::timestamptz,a.id,n.id\n"+
			" FROM shuttle_avatars a JOIN shuttle_avatar_deliveries n ON n.artifact_id=a.id AND "+
			"n.user_id=a.user_id\n"+
			" WHERE a.user_id=$2 AND a.draft_id IS NOT NULL AND n.receipt#>>'{ui,crop_opened_at}' "+
			"IS NOT NULL\n"+
			"\t ) actions JOIN shuttle_tasks task ON task.user_id=actions.user_id\n"+
			" LEFT JOIN shuttle_avatar_drafts d ON d.id=actions.draft_id AND "+
			"d.user_id=actions.user_id\n"+
			" LEFT JOIN shuttle_files f ON f.id=d.source_file_id AND f.user_id=actions.user_id\n"+
			" LEFT JOIN shuttle_avatar_deliveries n ON n.id=actions.delivery_id AND "+
			"n.user_id=actions.user_id\n"+
			" WHERE task.id=$1 AND task.user_id=$2 AND actions.created_at<task.created_at\n"+
			" ORDER BY actions.created_at DESC,actions.operation_id LIMIT 5",
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
		var artifact, delivery, source pgtype.UUID
		var opened pgtype.Timestamptz
		if err = rows.Scan(
			&a.OperationID,
			&a.DraftID,
			&a.Kind,
			&a.Version,
			&a.CompletedAt,
			&artifact,
			&delivery,
			&source,
			&a.SourceKind,
			&a.DeliveryState,
			&a.CropButtonOffered,
			&opened,
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
		if source.Valid {
			id := uuid.UUID(source.Bytes)
			a.SourceFileID = &id
		}
		if opened.Valid {
			at := opened.Time.UTC()
			a.CropOpenedAt = &at
		}
		a.CompletedAt = a.CompletedAt.UTC()
		result = append(result, a)
	}
	return result, rows.Err()
}
