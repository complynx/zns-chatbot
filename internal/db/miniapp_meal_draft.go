package db

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// SaveMiniAppMealDraft validates the session and journals the native action in
// the same draft/receipt transaction. The API must check current IAM first.
func (s *Store) SaveMiniAppMealDraft(
	ctx context.Context,
	deployment EventScope,
	bearer, generation [sha256.Size]byte,
	change MealDraftChange,
) (MealDraft, error) {
	a, hash, err := normalizeMealDraftChange(change)
	if err != nil {
		return MealDraft{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MealDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: deployment.BotID, BearerHash: bearer[:]},
	)
	if err != nil {
		return MealDraft{}, err
	}
	if err = q.LockEventOperation(ctx, fmt.Sprintf("meal-draft:%s:%s", initial.UserID, a.OperationID)); err != nil {
		return MealDraft{}, err
	}
	event, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{
			ID:             a.EventID,
			BotID:          deployment.BotID,
			OrganizationID: deployment.Organization,
		},
	)
	if err != nil {
		return MealDraft{}, err
	}
	attempt, err := s.lockMiniAppBearer(ctx, tx, deployment.BotID, bearer, generation)
	if err != nil {
		return MealDraft{}, err
	}
	if attempt.User.ID != initial.UserID || attempt.User.Organization != deployment.Organization {
		return MealDraft{}, ErrMiniAppDenied
	}
	scope := ProfileScope{
		BotID:        deployment.BotID,
		Organization: deployment.Organization,
		UserID:       attempt.User.ID,
		Subject:      attempt.User.Subject,
	}
	if err = authorizeMealDraft(ctx, tx, scope, a.EventID); err != nil {
		return MealDraft{}, err
	}
	result, replayed, err := s.saveMealDraft(ctx, tx, scope, event, a, hash)
	if err != nil || replayed {
		return result, err
	}
	err = q.InsertMiniAppMealDraftAction(
		ctx,
		sqlc.InsertMiniAppMealDraftActionParams{UserID: scope.UserID, OperationID: a.OperationID},
	)
	if err != nil {
		return MealDraft{}, err
	}
	return result, tx.Commit(ctx)
}
