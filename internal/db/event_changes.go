package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
)

const eventChangePreparationTTL = 5 * time.Minute

type EventChangePreparation struct {
	ID          uuid.UUID   `json:"proposal_id"`
	Arguments   EventChange `json:"arguments"`
	Description string      `json:"description"`
	ExpiresAt   time.Time   `json:"expires_at"`
}

func normalizedEventChange(args EventChange) (EventChange, []byte, error) {
	if args.ID == uuid.Nil || args.ExpectedVersion < 0 {
		return EventChange{}, nil, events.ErrInvalid
	}
	document, err := events.Normalize(args.Document)
	if err != nil {
		return EventChange{}, nil, err
	}
	args.Document = document
	raw, err := json.Marshal(args)
	hash := sha256.Sum256(raw)
	return args, hash[:], err
}

func eventChangeKey(scope AdminScope, op string) sqlc.GetEventChangeProposalParams {
	return sqlc.GetEventChangeProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
	}
}

func eventChangeDescription(before *events.Event, args EventChange) (string, error) {
	requested := events.Event{ID: args.ID, Document: args.Document}
	start, end, err := requested.LocalTimes()
	if err != nil {
		return "", err
	}
	action := "Create"
	var old events.Document
	if before != nil {
		action, old = "Replace", before.Document
	}
	removed := 0
	for _, module := range old.Modules {
		if !slices.Contains(args.Document.Modules, module) {
			removed++
		}
	}
	// Loom permits at most 4000 description bytes; exact parameters stay in Arguments.
	return fmt.Sprintf(
		"%s event %s at expected version %d; fully replace this event configuration.\nName (untrusted data): %q\nTimezone: %s; starts_at_local: %s; ends_at_local: %s.\nModules: %d -> %d; removed: %d. Registration configured: %t -> %t; pricing tiers: %d -> %d; deposit configured: %t -> %t.\nThe complete exact requested parameters are in arguments. Omitted registration/deposit removes that future policy; omitted modules are disabled. This does not register users, assign or cancel passes, change frozen pass terms, or credit money. Current module access is rechecked at execution.",
		action,
		args.ID,
		args.ExpectedVersion,
		args.Document.Name,
		args.Document.Timezone,
		start.Format(time.RFC3339Nano),
		end.Format(time.RFC3339Nano),
		len(old.Modules),
		len(args.Document.Modules),
		removed,
		old.Registration != nil,
		args.Document.Registration != nil,
		eventTierCount(old),
		eventTierCount(args.Document),
		old.Deposit != nil,
		args.Document.Deposit != nil,
	), nil
}

func eventTierCount(document events.Document) int {
	if document.Registration != nil && document.Registration.Pricing != nil {
		return len(document.Registration.Pricing.Tiers)
	}
	return 0
}

// PrepareEventChange persists exact approval parameters without changing an event.
func (s *Store) PrepareEventChange(
	ctx context.Context, scope AdminScope, op string, args EventChange,
) (EventChangePreparation, error) {
	args, hash, err := normalizedEventChange(args)
	if err != nil {
		return EventChangePreparation{}, err
	}
	tx, q, err := s.beginAdminOperation(ctx, scope, "event-change", op)
	if err != nil {
		return EventChangePreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetEventChangeProposal(ctx, eventChangeKey(scope, op))
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return EventChangePreparation{}, ErrConflict
		}
		var prep EventChangePreparation
		err = json.Unmarshal(prior.Document, &prep)
		return prep, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return EventChangePreparation{}, err
	}
	row, err := q.GetEventConfiguration(ctx, sqlc.GetEventConfigurationParams{
		ID: args.ID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	var before *events.Event
	if err == nil {
		if args.ExpectedVersion == 0 || row.ShuttleEvent.Version != args.ExpectedVersion {
			return EventChangePreparation{}, ErrConflict
		}
		current, decodeErr := eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
		if decodeErr != nil {
			return EventChangePreparation{}, decodeErr
		}
		before = &current
	} else if !errors.Is(err, pgx.ErrNoRows) || args.ExpectedVersion != 0 {
		return EventChangePreparation{}, err
	}
	description, err := eventChangeDescription(before, args)
	if err != nil {
		return EventChangePreparation{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return EventChangePreparation{}, err
	}
	prep := EventChangePreparation{
		ID: uuid.New(), Arguments: args, Description: description,
		ExpiresAt: now.Time.Add(eventChangePreparationTTL),
	}
	document, err := json.Marshal(prep)
	if err != nil {
		return EventChangePreparation{}, err
	}
	err = q.InsertEventChangeProposal(ctx, sqlc.InsertEventChangeProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
		ProposalID: prep.ID, RequestHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: prep.ExpiresAt, Valid: true}, Document: document,
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return prep, err
}

// ConfirmEventChange commits configuration, child policies and its original agent result together.
// Operator receipts are a separate namespace and never establish agent approval.
func (s *Store) ConfirmEventChange(
	ctx context.Context, scope AdminScope, op string, args EventChange, proposal uuid.UUID,
) (events.Event, error) {
	args, hash, err := normalizedEventChange(args)
	if err != nil {
		return events.Event{}, err
	}
	if proposal == uuid.Nil {
		return events.Event{}, events.ErrInvalid
	}
	tx, q, err := s.beginAdminOperation(ctx, scope, "event-change", op)
	if err != nil {
		return events.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetEventChangeProposal(ctx, eventChangeKey(scope, op))
	if err != nil {
		return events.Event{}, err
	}
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return events.Event{}, ErrConflict
	}
	if len(prior.Result) != 0 {
		var result events.Event
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	row, err := writeEvent(ctx, q, EventScope{BotID: scope.BotID, Organization: scope.Organization}, args)
	if err != nil {
		return events.Event{}, err
	}
	// An update row lock or competing creation can block. Expiry is checked after both waits.
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return events.Event{}, err
	}
	if !now.Time.Before(prior.ExpiresAt.Time) {
		return events.Event{}, ErrConflict
	}
	result, err := writeEventPolicies(ctx, q, row, args.Document)
	if err != nil {
		return events.Event{}, err
	}
	receipt, err := json.Marshal(result)
	if err != nil {
		return events.Event{}, err
	}
	saved, err := q.SaveEventChangeResult(ctx, sqlc.SaveEventChangeResultParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op, Result: receipt,
	})
	if err == nil && saved != 1 {
		err = ErrConflict
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

// EventChangeReceipt reads only this currently active administrator's original agent result.
func (s *Store) EventChangeReceipt(ctx context.Context, scope AdminScope, op string) (*events.Event, error) {
	tx, q, err := s.beginAdminOperation(ctx, scope, "event-change", op)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := q.GetEventChangeProposal(ctx, eventChangeKey(scope, op))
	if err != nil {
		return nil, err
	}
	if len(row.Result) == 0 {
		return nil, pgx.ErrNoRows
	}
	var result events.Event
	err = json.Unmarshal(row.Result, &result)
	return &result, err
}
