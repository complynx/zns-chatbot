package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// OwnMealDraftOperationByID recovers an original receipt for Loom status probes.
// Resolve its event privately, release the owner lock, then recheck current
// entitlement in event -> owner order before returning any receipt data.
func (s *Store) OwnMealDraftOperationByID(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MealDraft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MealDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return MealDraft{}, err
	}
	row, err := s.q.WithTx(tx).
		ReadMealDraftOperation(ctx, sqlc.ReadMealDraftOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return MealDraft{}, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return MealDraft{}, err
	}
	return s.OwnMealDraftOperation(ctx, scope, row.EventID, operation)
}
