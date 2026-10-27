package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

var errInvalidStoredRegistrationRules = errors.New("invalid stored registration rules")

type EventScope struct {
	BotID        int64
	Organization string
}
type EventChange struct {
	ID              uuid.UUID       `json:"id"`
	ExpectedVersion int64           `json:"expected_version"`
	Document        events.Document `json:"document"`
}

func eventRecord(row sqlc.ShuttleEvent, rawRules, rawDeposit []byte) (events.Event, error) {
	var rules *registrations.Rules
	if len(rawRules) > 0 {
		if err := json.Unmarshal(rawRules, &rules); err != nil {
			return events.Event{}, errInvalidStoredRegistrationRules
		}
		if rules != nil {
			normalized, err := rules.Normalize()
			if err != nil || normalized.ClosesAt.After(row.EndsAt.Time) {
				return events.Event{}, errInvalidStoredRegistrationRules
			}
			rules = &normalized
		}
	}
	var deposit *registrations.DepositPolicy
	if len(rawDeposit) > 0 {
		if err := json.Unmarshal(rawDeposit, &deposit); err != nil {
			return events.Event{}, errInvalidStoredRegistrationRules
		}
	}
	if deposit != nil {
		if rules == nil {
			return events.Event{}, errInvalidStoredRegistrationRules
		}
		if err := deposit.ValidatePrices(*rules); err != nil {
			return events.Event{}, errInvalidStoredRegistrationRules
		}
	}
	return events.Event{ID: row.ID, Version: row.Version, Document: events.Document{
		Registration: rules,
		Deposit:      deposit,
		Name:         row.Name,
		Timezone:     row.Timezone,
		StartsAt:     row.StartsAt.Time.UTC(),
		EndsAt:       row.EndsAt.Time.UTC(),
		Modules:      row.Modules,
	}}, nil
}

func (s *Store) Event(ctx context.Context, scope EventScope, id uuid.UUID) (events.Event, error) {
	row, err := s.q.GetEventConfiguration(
		ctx,
		sqlc.GetEventConfigurationParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return events.Event{}, err
	}
	return eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
}

func (s *Store) Events(ctx context.Context, scope EventScope, after uuid.UUID, limit int32) ([]events.Event, error) {
	if limit < 1 || limit > 100 {
		return nil, events.ErrInvalid
	}
	rows, err := s.q.ListEventConfigurations(
		ctx,
		sqlc.ListEventConfigurationsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ID:             after,
			Limit:          limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]events.Event, 0, len(rows))
	for _, row := range rows {
		event, decodeErr := eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, event)
	}
	return result, nil
}

// ChangeEvent commits configuration and its immutable operation receipt together.
// Replaying the same normalized request returns the original result, even after later edits.
func (s *Store) ChangeEvent(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	change EventChange,
) (events.Event, error) {
	if scope.BotID <= 0 || scope.Organization == "" || actor == "" || operation == "" || len(operation) > 128 ||
		change.ID == uuid.Nil ||
		change.ExpectedVersion < 0 {
		return events.Event{}, events.ErrInvalid
	}
	document, err := events.Normalize(change.Document)
	if err != nil {
		return events.Event{}, err
	}
	change.Document = document
	request, err := json.Marshal(change)
	if err != nil {
		return events.Event{}, err
	}
	digest := sha256.Sum256(request)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return events.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("event:%d:%s:%s:%s", scope.BotID, scope.Organization, actor, operation),
	); err != nil {
		return events.Event{}, err
	}
	key := sqlc.GetEventOperationParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          actor,
		OperationID:    operation,
	}
	receipt, err := q.GetEventOperation(ctx, key)
	if err == nil {
		if !bytes.Equal(receipt.RequestHash, digest[:]) {
			return events.Event{}, ErrConflict
		}
		var result events.Event
		err = json.Unmarshal(receipt.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, err
	}
	row, err := writeEvent(ctx, q, scope, change)
	if err != nil {
		return events.Event{}, err
	}
	result, err := writeEventPolicies(ctx, q, row, document)
	if err != nil {
		return events.Event{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return events.Event{}, err
	}
	err = q.InsertEventOperation(
		ctx,
		sqlc.InsertEventOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
			RequestHash:    digest[:],
			Result:         payload,
		},
	)
	if err != nil {
		return events.Event{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return events.Event{}, err
	}
	return result, nil
}

// writeEventPolicies stores child configuration under the event row lock.
func writeEventPolicies(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ShuttleEvent,
	document events.Document,
) (events.Event, error) {
	var err error
	var ruleJSON []byte
	if document.Registration != nil {
		ruleJSON, err = json.Marshal(document.Registration)
		if err != nil {
			return events.Event{}, err
		}
	}
	if err = q.SetEventRegistrationRules(
		ctx,
		sqlc.SetEventRegistrationRulesParams{EventID: row.ID, Document: ruleJSON},
	); err != nil {
		return events.Event{}, err
	}
	var depositJSON []byte
	if document.Deposit != nil {
		depositJSON, err = json.Marshal(document.Deposit)
		if err != nil {
			return events.Event{}, err
		}
	}
	if err = q.SetEventDepositPolicy(
		ctx,
		sqlc.SetEventDepositPolicyParams{EventID: row.ID, Document: depositJSON},
	); err != nil {
		return events.Event{}, err
	}
	return eventRecord(row, ruleJSON, depositJSON)
}

func writeEvent(ctx context.Context, q *sqlc.Queries, scope EventScope, change EventChange) (sqlc.ShuttleEvent, error) {
	d := change.Document
	start := pgtype.Timestamptz{Time: d.StartsAt, Valid: true}
	end := pgtype.Timestamptz{Time: d.EndsAt, Valid: true}
	var err error
	var row sqlc.ShuttleEvent
	if change.ExpectedVersion == 0 {
		row, err = q.CreateEvent(
			ctx,
			sqlc.CreateEventParams{
				ID:             change.ID,
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				Name:           d.Name,
				Timezone:       d.Timezone,
				StartsAt:       start,
				EndsAt:         end,
				Modules:        d.Modules,
			},
		)
	} else {
		_, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: change.ID, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
		if err != nil {
			return row, err
		}
		conflicts, checkErr := q.MassageBookingsOutsideEvent(
			ctx,
			sqlc.MassageBookingsOutsideEventParams{EventID: change.ID, StartsAt: start, EndsAt: end},
		)
		if checkErr != nil {
			return row, checkErr
		}
		if conflicts {
			return row, ErrConflict
		}
		row, err = q.UpdateEvent(
			ctx,
			sqlc.UpdateEventParams{
				ID:             change.ID,
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				Version:        change.ExpectedVersion,
				Name:           d.Name,
				Timezone:       d.Timezone,
				StartsAt:       start,
				EndsAt:         end,
				Modules:        d.Modules,
			},
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrConflict
	}
	return row, err
}

func (s *Store) EventOperation(ctx context.Context, scope EventScope, actor, operation string) (events.Event, error) {
	receipt, err := s.q.GetEventOperation(
		ctx,
		sqlc.GetEventOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err != nil {
		return events.Event{}, err
	}
	var result events.Event
	err = json.Unmarshal(receipt.Result, &result)
	return result, err
}
