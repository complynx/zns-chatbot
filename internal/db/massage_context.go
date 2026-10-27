package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// MassageAction describes the original effect, not the current booking state.
type MassageAction struct {
	OperationID   uuid.UUID            `json:"operation_id"`
	EventID       uuid.UUID            `json:"event_id"`
	Kind          string               `json:"kind"`
	Source        MassageBookingSource `json:"source"`
	ProposalID    uuid.UUID            `json:"proposal_id"`
	BookingID     uuid.UUID            `json:"booking_id"`
	OriginalState string               `json:"original_state"`
	RecordedAt    time.Time            `json:"recorded_at"`
}

type MassageReactiveContext struct {
	Pending []MassagePendingProposal `json:"pending"`
	Actions []MassageAction          `json:"actions"`
}

// RecentMassageContext supplies at most five active proposals and five receipts
// created before this owner's reactive task. It does not authorize any effect.
func (s *Store) RecentMassageContext(ctx context.Context, scope ProfileScope, taskID uuid.UUID,
) (MassageReactiveContext, error) {
	result := MassageReactiveContext{Pending: []MassagePendingProposal{}, Actions: []MassageAction{}}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, uuid.Nil)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	pending, err := q.ListTaskPendingMassagePreparations(ctx, sqlc.ListTaskPendingMassagePreparationsParams{
		TaskID: taskID, UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return result, err
	}
	for _, row := range pending {
		item, readErr := massagePendingProposal(row)
		if readErr != nil {
			return result, readErr
		}
		result.Pending = append(result.Pending, item)
	}
	actions, err := q.ListTaskMassageOperations(ctx, sqlc.ListTaskMassageOperationsParams{
		TaskID: taskID, UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return result, err
	}
	for _, row := range actions {
		result.Actions = append(result.Actions, MassageAction{
			OperationID:   row.OperationID,
			EventID:       row.EventID,
			Kind:          row.Kind,
			Source:        MassageBookingSource(row.Source),
			ProposalID:    row.PreparationID,
			BookingID:     row.BookingID,
			OriginalState: row.OriginalState,
			RecordedAt:    row.CreatedAt.Time.UTC(),
		})
	}
	return result, nil
}
