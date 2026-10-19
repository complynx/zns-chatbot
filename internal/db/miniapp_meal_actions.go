package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

type MiniAppMealDraftAction struct {
	OperationID    uuid.UUID `json:"operation_id"`
	EventID        uuid.UUID `json:"event_id"`
	DraftVersion   int64     `json:"draft_version"`
	EventVersion   int64     `json:"event_version"`
	CatalogVersion int64     `json:"catalog_version"`
	Cleared        bool      `json:"cleared"`
	CompletedAt    time.Time `json:"completed_at"`
}

// RecentMiniAppMealDraftActions returns at most five original native receipts
// preceding this owner's reactive task, restricted to currently entitled events.
// It neither starts a task nor treats historical metadata as current intent.
func (s *Store) RecentMiniAppMealDraftActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]MiniAppMealDraftAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := s.q.WithTx(tx).
		RecentMiniAppMealDraftActions(ctx, sqlc.RecentMiniAppMealDraftActionsParams{ID: taskID, UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization})
	if err != nil {
		return nil, err
	}
	result := make([]MiniAppMealDraftAction, 0, len(rows))
	for _, r := range rows {
		result = append(
			result,
			MiniAppMealDraftAction{
				OperationID:    r.OperationID,
				EventID:        r.EventID,
				DraftVersion:   r.DraftVersion,
				EventVersion:   r.EventVersion,
				CatalogVersion: r.CatalogVersion,
				Cleared:        r.Cleared,
				CompletedAt:    r.CompletedAt.Time.UTC(),
			},
		)
	}
	return result, nil
}
