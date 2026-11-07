package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// HistoryAvatarCandidates lists private import locators, not validated images.
// Selection validates the archive and image type before any Telegram request.
type HistoryAvatarCandidates struct {
	Items   []AvatarHistorySource `json:"items"`
	HasMore bool                  `json:"has_more"`
	NextID  uuid.UUID             `json:"next_id,omitempty"`
}

func (s *Store) OwnerHistoryAvatarCandidates(ctx context.Context, scope ProfileScope,
	after uuid.UUID, limit int32,
) (HistoryAvatarCandidates, error) {
	result := HistoryAvatarCandidates{Items: []AvatarHistorySource{}}
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return result, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return result, err
	}
	rows, err := q.OwnerHistoryAvatarCandidates(ctx, sqlc.OwnerHistoryAvatarCandidatesParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, OwnerTelegramID: owner, ID: after, Limit: limit + 1,
	})
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		if len(result.Items) == int(limit) {
			result.HasMore = true
			result.NextID = result.Items[len(result.Items)-1].RecordID
			break
		}
		result.Items = append(result.Items, AvatarHistorySource{RecordID: row.ID, ArchiveID: row.FirstArchiveID})
	}
	return result, nil
}
