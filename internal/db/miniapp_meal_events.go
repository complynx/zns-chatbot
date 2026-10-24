package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
)

// ListOwnMealDraftEvents includes entitled past events for saved history.
// It does not require a configured catalog, an open sale, or a paid pass.
func (s *Store) ListOwnMealDraftEvents(
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
	rows, err := s.q.WithTx(tx).ListOwnMealDraftEvents(ctx, sqlc.ListOwnMealDraftEventsParams{
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
