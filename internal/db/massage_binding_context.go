package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

type MassageBindingReactiveContext struct {
	Pending []MassageBindingProposalStatus `json:"pending"`
	Actions []MassageBindingProposalStatus `json:"actions"`
}

// HasMassageBindingContext returns only this authenticated actor's history bit.
// It avoids role verification when no binding history precedes the reactive task.
func (s *Store) HasMassageBindingContext(ctx context.Context, scope ProfileScope, taskID uuid.UUID) (bool, error) {
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, nil, uuid.Nil)
	if err != nil {
		return false, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return q.HasTaskMassageBindingContext(ctx, sqlc.HasTaskMassageBindingContextParams{
		TaskID: taskID, ActorID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
}

// RecentMassageBindingContext requires current global administrator authorization
// at the caller. It returns only this actor's canonical proposals and receipts
// recorded before the task; aliases cannot duplicate context or renew consent.
func (s *Store) RecentMassageBindingContext(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) (MassageBindingReactiveContext, error) {
	result := MassageBindingReactiveContext{
		Pending: []MassageBindingProposalStatus{},
		Actions: []MassageBindingProposalStatus{},
	}
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, nil, uuid.Nil)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	pending, err := q.ListTaskPendingMassageBindings(ctx, sqlc.ListTaskPendingMassageBindingsParams{
		TaskID: taskID, ActorID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return result, err
	}
	completed, err := q.ListTaskCompletedMassageBindings(ctx, sqlc.ListTaskCompletedMassageBindingsParams{
		TaskID: taskID, ActorID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	for _, row := range pending {
		item, readErr := massageBindingStatus(row, now.Time)
		if readErr != nil {
			return result, readErr
		}
		if item.State == miniAppPending {
			result.Pending = append(result.Pending, item)
		}
	}
	for _, row := range completed {
		item, readErr := massageBindingStatus(row, now.Time)
		if readErr != nil {
			return result, readErr
		}
		result.Actions = append(result.Actions, item)
	}
	return result, nil
}
