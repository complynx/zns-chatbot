package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// MealCatalogOperationByID recovers the current actor's administrative receipt.
// The API caller must check current global administrator authority.
// Resolve its event privately, release the owner lock, then recheck current
// account binding in event -> owner order before returning any receipt data.
func (s *Store) MealCatalogOperationByID(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MealCatalog, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MealCatalog{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return MealCatalog{}, err
	}
	row, err := s.q.WithTx(tx).
		ReadMealCatalogOperation(ctx, sqlc.ReadMealCatalogOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return MealCatalog{}, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return MealCatalog{}, err
	}
	return s.MealCatalogOperation(ctx, scope, row.EventID, operation)
}
