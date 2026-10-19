package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func mealCatalogVersion(ctx context.Context, q *sqlc.Queries, event uuid.UUID, expected int64) error {
	catalog, err := readMealCatalog(ctx, q, event, 0)
	if errors.Is(err, pgx.ErrNoRows) && expected == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if catalog.Version != expected {
		return ErrConflict
	}
	return nil
}

// PrepareMealCatalog freezes one five-minute exact configuration proposal.
// The API caller checks global administration; the store checks current binding.
func (s *Store) PrepareMealCatalog(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	change MealCatalogChange,
) (MealCatalogPreparation, error) {
	a, hash, err := normalizeMealCatalogChange(change)
	if err != nil || operation == uuid.Nil {
		return MealCatalogPreparation{}, orders.ErrMealCatalog
	}
	tx, event, err := s.mealEventTx(ctx, scope, a.EventID, operation, "meal-catalog")
	if err != nil {
		return MealCatalogPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	prior, err := q.ReadMealCatalogPreparation(
		ctx,
		sqlc.ReadMealCatalogPreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if prior.EventID != a.EventID || !bytes.Equal(prior.RequestHash, hash) {
			return MealCatalogPreparation{}, ErrConflict
		}
		return MealCatalogPreparation{ID: prior.ID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return MealCatalogPreparation{}, err
	}
	if event.Version != a.ExpectedEventVersion {
		return MealCatalogPreparation{}, ErrConflict
	}
	if err = mealCatalogVersion(ctx, q, a.EventID, a.ExpectedVersion); err != nil {
		return MealCatalogPreparation{}, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return MealCatalogPreparation{}, err
	}
	row, err := q.InsertMealCatalogPreparation(
		ctx,
		sqlc.InsertMealCatalogPreparationParams{
			ID:          uuid.New(),
			UserID:      scope.UserID,
			OperationID: operation,
			EventID:     a.EventID,
			RequestHash: hash,
			Arguments:   raw,
		},
	)
	if err != nil {
		return MealCatalogPreparation{}, err
	}
	result := MealCatalogPreparation{ID: row.ID, Arguments: a, ExpiresAt: row.ExpiresAt.Time.UTC()}
	return result, tx.Commit(ctx)
}

// ChangeMealCatalog appends immutable terms and the original operation receipt.
// checkIdentity is a fresh authority check before the first effect; API auth is
// also required for replays. No draft is rewritten or repriced by publication.
func (s *Store) ChangeMealCatalog(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	change MealCatalogChange,
	checkIdentity func(context.Context) error,
) (MealCatalog, error) {
	a, hash, err := normalizeMealCatalogChange(change)
	if err != nil || operation == uuid.Nil || proposal == uuid.Nil || checkIdentity == nil {
		return MealCatalog{}, orders.ErrMealCatalog
	}
	tx, event, err := s.mealEventTx(ctx, scope, a.EventID, operation, "meal-catalog")
	if err != nil {
		return MealCatalog{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	prior, err := q.ReadMealCatalogOperation(
		ctx,
		sqlc.ReadMealCatalogOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if prior.EventID != a.EventID || !bytes.Equal(prior.RequestHash, hash) {
			return MealCatalog{}, ErrConflict
		}
		var result MealCatalog
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return MealCatalog{}, err
	}
	prepared, err := q.ReadMealCatalogPreparation(
		ctx,
		sqlc.ReadMealCatalogPreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return MealCatalog{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return MealCatalog{}, err
	}
	if prepared.ID != proposal || prepared.EventID != a.EventID || !bytes.Equal(prepared.RequestHash, hash) ||
		!prepared.ExpiresAt.Time.After(now.Time) ||
		event.Version != a.ExpectedEventVersion {
		return MealCatalog{}, ErrConflict
	}
	if err = mealCatalogVersion(ctx, q, a.EventID, a.ExpectedVersion); err != nil {
		return MealCatalog{}, err
	}
	if err = checkIdentity(ctx); err != nil {
		return MealCatalog{}, err
	}
	// Authority verification may wait on IAM. It cannot extend exact consent.
	now, err = q.RegistrationNow(ctx)
	if err != nil {
		return MealCatalog{}, err
	}
	if !prepared.ExpiresAt.Time.After(now.Time) {
		return MealCatalog{}, ErrConflict
	}
	raw, err := json.Marshal(a.Document)
	if err != nil {
		return MealCatalog{}, err
	}
	row, err := q.InsertMealCatalog(
		ctx,
		sqlc.InsertMealCatalogParams{EventID: a.EventID, Version: a.ExpectedVersion + 1, Document: raw},
	)
	if err != nil {
		return MealCatalog{}, err
	}
	result := MealCatalog{EventID: row.EventID, Version: row.Version, Document: a.Document}
	receipt, err := json.Marshal(result)
	if err != nil {
		return MealCatalog{}, err
	}
	err = q.InsertMealCatalogOperation(
		ctx,
		sqlc.InsertMealCatalogOperationParams{
			UserID:      scope.UserID,
			OperationID: operation,
			EventID:     a.EventID,
			RequestHash: hash,
			Result:      receipt,
		},
	)
	if err != nil {
		return MealCatalog{}, err
	}
	return result, tx.Commit(ctx)
}

// MealCatalogOperation recovers the actor's receipt within its explicit event.
// The caller must still hold current API administrator authority.
func (s *Store) MealCatalogOperation(
	ctx context.Context,
	scope ProfileScope,
	event, operation uuid.UUID,
) (MealCatalog, error) {
	tx, _, err := s.mealEventTx(ctx, scope, event, uuid.Nil, "meal-catalog")
	if err != nil {
		return MealCatalog{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		ReadMealCatalogOperation(ctx, sqlc.ReadMealCatalogOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return MealCatalog{}, err
	}
	if row.EventID != event {
		return MealCatalog{}, pgx.ErrNoRows
	}
	var result MealCatalog
	err = json.Unmarshal(row.Result, &result)
	return result, err
}
