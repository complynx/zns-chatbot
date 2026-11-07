package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// MassageSpecialistPreferenceAction is an original receipt, not a current setting.
type MassageSpecialistPreferenceAction struct {
	OperationID    uuid.UUID                    `json:"operation_id"`
	EventID        uuid.UUID                    `json:"event_id"`
	Source         MassageBookingSource         `json:"source"`
	OriginalResult MassageSpecialistPreferences `json:"original_result"`
	RecordedAt     time.Time                    `json:"recorded_at"`
}

// RecentMassageSpecialistPreferenceActions returns at most five own receipts
// before this reactive task, filtered by current event/module/specialist access.
func (s *Store) RecentMassageSpecialistPreferenceActions(ctx context.Context, scope ProfileScope,
	taskID uuid.UUID,
) ([]MassageSpecialistPreferenceAction, error) {
	result := []MassageSpecialistPreferenceAction{}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, uuid.Nil)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListTaskMassageSpecialistPreferenceOperations(
		ctx,
		sqlc.ListTaskMassageSpecialistPreferenceOperationsParams{
			TaskID: taskID, UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
		},
	)
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		receipt, decodeErr := decodeMassageSpecialistPreference(row.Result)
		if decodeErr != nil {
			return result, decodeErr
		}
		result = append(result, MassageSpecialistPreferenceAction{
			OperationID: row.OperationID, EventID: row.EventID, Source: MassageBookingSource(row.Source),
			OriginalResult: receipt, RecordedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return result, nil
}
