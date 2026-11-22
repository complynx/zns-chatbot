package db

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
)

// PassEntryEvent lets a participant inspect the sale window before registration opens.
func (s *Store) PassEntryEvent(
	ctx context.Context,
	scope EventContextScope,
	id uuid.UUID,
) (events.Event, time.Time, error) {
	if !scope.valid() || id == uuid.Nil {
		return events.Event{}, time.Time{}, events.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return events.Event{}, time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = contextUser(ctx, q, scope); err != nil {
		return events.Event{}, time.Time{}, err
	}
	row, err := q.GetEventConfiguration(
		ctx,
		sqlc.GetEventConfigurationParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return events.Event{}, time.Time{}, err
	}
	event, err := eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
	if err != nil {
		return events.Event{}, time.Time{}, err
	}
	if event.Document.Registration == nil || !slices.Contains(event.Document.Modules, "registrations") {
		return events.Event{}, time.Time{}, pgx.ErrNoRows
	}
	now, err := q.RegistrationNow(ctx)
	return event, now.Time, err
}
