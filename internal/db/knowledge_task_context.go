package db

import (
	"context"

	"github.com/google/uuid"
)

// KnowledgeActionsForTask returns actor-owned history before this task was
// accepted. Retrying the task cannot move the cutoff to newer user actions.
func (s *Store) KnowledgeActionsForTask(ctx context.Context, scope PaymentScope,
	taskID uuid.UUID,
) ([]KnowledgeRecentAction, error) {
	task, err := s.q.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != scope.UserID {
		return nil, ErrKnowledgeDenied
	}
	return s.RecentKnowledgeActions(ctx, scope, task.CreatedAt.Time)
}
