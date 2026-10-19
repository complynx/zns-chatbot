package db

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
)

// ListOwnOrdersSelectionEvents includes entitled past events for saved history.
// It does not require a configured catalog, an open sale, or a paid pass.
func (s *Store) ListOwnOrdersSelectionEvents(
	ctx context.Context,
	scope ProfileScope,
	after uuid.UUID,
) (events.ContextList, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return events.ContextList{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return events.ContextList{}, err
	}
	rows, err := s.q.WithTx(tx).ListOwnOrdersSelectionEvents(ctx, sqlc.ListOwnOrdersSelectionEventsParams{
		UserID:         scope.UserID,
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		ID:             after,
		Limit:          events.ContextChoiceLimit + 1,
	})
	if err != nil {
		return events.ContextList{}, err
	}
	result := events.ContextList{Events: []events.ContextEvent{}}
	more := len(rows) > events.ContextChoiceLimit
	if more {
		rows = rows[:events.ContextChoiceLimit]
	}
	for _, row := range rows {
		result.Events = append(
			result.Events,
			events.ContextEvent{ID: row.ID, Version: row.Version, Name: row.Name, Timezone: row.Timezone,
				StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC()},
		)
	}
	if more {
		result.Next = &result.Events[len(result.Events)-1].ID
	}
	return result, nil
}

// SaveMiniAppOrdersSelection checks the session in the draft/receipt/journal
// transaction. IAM must be checked before entry. Event -> owner -> session order
// matches agent writes and logout; the initial bearer lookup takes no row locks.
func (s *Store) SaveMiniAppOrdersSelection(
	ctx context.Context,
	deployment EventScope,
	bearer, generation [sha256.Size]byte,
	change OrdersSelectionChange,
) (OrdersSelection, error) {
	a, hash, err := normalizeOrdersSelection(change)
	if err != nil {
		return OrdersSelection{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrdersSelection{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: deployment.BotID, BearerHash: bearer[:]},
	)
	if err != nil {
		return OrdersSelection{}, err
	}
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("orders-selection:%s:%s", initial.UserID, a.OperationID),
	); err != nil {
		return OrdersSelection{}, err
	}
	_, err = q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{
			ID:             a.EventID,
			BotID:          deployment.BotID,
			OrganizationID: deployment.Organization,
		},
	)
	if err != nil {
		return OrdersSelection{}, err
	}
	attempt, err := s.lockMiniAppBearer(ctx, tx, deployment.BotID, bearer, generation)
	if err != nil {
		return OrdersSelection{}, err
	}
	if attempt.User.ID != initial.UserID || attempt.User.Organization != deployment.Organization {
		return OrdersSelection{}, ErrMiniAppDenied
	}
	scope := ProfileScope{
		BotID:        deployment.BotID,
		Organization: deployment.Organization,
		UserID:       attempt.User.ID,
		Subject:      attempt.User.Subject,
	}
	if err = authorizeOrdersSelection(ctx, tx, scope, a.EventID); err != nil {
		return OrdersSelection{}, err
	}
	result, replayed, err := s.saveOrdersSelection(ctx, tx, scope, a, hash)
	if err != nil || replayed {
		return result, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_miniapp_orders_selection_actions(user_id,operation_id) VALUES($1,$2)`,
		scope.UserID,
		a.OperationID,
	)
	if err != nil {
		return OrdersSelection{}, err
	}
	return result, tx.Commit(ctx)
}
