package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const assignmentPreparationTTL = 5 * time.Minute

// PassAssignment identifies the registration and the versions approved by an operator.
type PassAssignment struct {
	EventID              uuid.UUID `json:"event_id"`
	RegistrationID       uuid.UUID `json:"registration_id"`
	ExpectedEventVersion int64     `json:"expected_event_version" minimum:"1"`
	ExpectedVersion      int64     `json:"expected_version"       minimum:"1"`
}

func (a PassAssignment) change() RegistrationChange {
	return RegistrationChange{
		Kind:                 "assign",
		EventID:              a.EventID,
		RegistrationID:       a.RegistrationID,
		ExpectedEventVersion: a.ExpectedEventVersion,
		ExpectedVersion:      a.ExpectedVersion,
	}
}

type AssignmentPreparation struct {
	Quotes      []AssignmentQuote `json:"quotes,omitempty"`
	ID          uuid.UUID         `json:"proposal_id"`
	Arguments   PassAssignment    `json:"arguments"`
	Description string            `json:"description"`
	ExpiresAt   time.Time         `json:"expires_at"`
}

// PrepareAssignment stores an immutable proposal without assigning or reserving a pass.
// Exact repeats preserve the description and expiry; execution rechecks capacity and versions.
func (s *Store) PrepareAssignment(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	args PassAssignment,
) (AssignmentPreparation, error) {
	return s.prepareAssignment(ctx, scope, actor, operation, args, uuid.Nil)
}

func (s *Store) prepareAssignment(ctx context.Context, scope EventScope, actor, operation string,
	args PassAssignment, resume uuid.UUID,
) (AssignmentPreparation, error) {
	if scope.BotID <= 0 || scope.Organization == "" || actor == "" || operation == "" || len(operation) > 128 ||
		!validRegistrationChange(args.change()) {
		return AssignmentPreparation{}, registrations.ErrInvalid
	}
	raw, err := json.Marshal(args.change())
	if err != nil {
		return AssignmentPreparation{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("registration:%d:%s:%s:%s", scope.BotID, scope.Organization, actor, operation),
	); err != nil {
		return AssignmentPreparation{}, err
	}
	if resume != uuid.Nil {
		if err = lockAssignmentEvent(ctx, q, scope, args.EventID); err != nil {
			return AssignmentPreparation{}, err
		}
	}
	prior, err := q.GetAssignmentPreparation(
		ctx,
		sqlc.GetAssignmentPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err == nil {
		return replayAssignmentPreparation(ctx, q, prior, hash[:], resume)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AssignmentPreparation{}, err
	}
	if err = requireUnusedRegistrationOperation(ctx, q, scope, actor, operation); err != nil {
		return AssignmentPreparation{}, err
	}

	if resume != uuid.Nil {
		result, adoptErr := adoptAssignment(ctx, q, scope, actor, operation, resume, hash[:])
		if adoptErr != nil {
			return AssignmentPreparation{}, adoptErr
		}
		if err = tx.Commit(ctx); err != nil {
			return AssignmentPreparation{}, err
		}
		return result, nil
	}
	result, err := buildAssignmentPreparation(ctx, q, scope, args)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	document, err := json.Marshal(result)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if err = q.InsertAssignmentPreparation(
		ctx,
		sqlc.InsertAssignmentPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
			ProposalID:     result.ID,
			RequestHash:    hash[:],
			ExpiresAt:      pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
			Document:       document,
		},
	); err != nil {
		return AssignmentPreparation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AssignmentPreparation{}, err
	}
	return result, nil
}

func buildAssignmentPreparation(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	args PassAssignment,
) (AssignmentPreparation, error) {
	event, err := registrationEventForChange(ctx, q, scope, args.EventID)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if event.Version != args.ExpectedEventVersion {
		return AssignmentPreparation{}, ErrConflict
	}
	entry, err := q.GetRegistrationByID(
		ctx,
		sqlc.GetRegistrationByIDParams{ID: args.RegistrationID, EventID: args.EventID},
	)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if err = pairAssignmentAvailable(ctx, q, entry.ID); err != nil {
		return AssignmentPreparation{}, err
	}
	if entry.Version != args.ExpectedVersion || entry.State != registrations.Waitlisted {
		return AssignmentPreparation{}, ErrConflict
	}
	user, err := q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: entry.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if err = requireRegistrationDetails(ctx, q, event, entry.UserID); err != nil {
		return AssignmentPreparation{}, err
	}
	assigned, err := q.CountAssignedRole(ctx, sqlc.CountAssignedRoleParams{EventID: args.EventID, Role: entry.Role})
	if err != nil {
		return AssignmentPreparation{}, err
	}
	rules := event.Document.Registration
	capacity, err := rules.Capacity(entry.Role)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if assigned >= int64(capacity) {
		return AssignmentPreparation{}, registrations.ErrCapacity
	}
	now, err := registrationMutationTime(ctx, q, rules.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return AssignmentPreparation{}, err
	}
	zone, err := time.LoadLocation(event.Document.Timezone)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	prices, err := assignmentPrices(ctx, q, event, now.Time, []string{entry.Role})
	if err != nil {
		return AssignmentPreparation{}, err
	}
	quotes, err := assignmentQuotes(event, prices, entry.ID)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	description := fmt.Sprintf(
		"Assign an unpaid %s pass to Telegram user %d for %s (event %s). Price %s %s. Payment window: %d seconds after assignment, capped at %s. No payment is recorded.",
		entry.Role,
		user.TgUserID,
		event.Document.Name,
		event.ID,
		minorAmount(quotes[0].PriceMinor, rules.CurrencyScale),
		rules.Currency,
		rules.PaymentWindowSeconds,
		rules.ClosesAt.In(zone).Format(time.RFC3339),
	)
	if quotes[0].Tier != nil {
		description += fmt.Sprintf(" Tier %d (%s).", quotes[0].Tier.Number, quotes[0].Tier.ID)
	}
	if event.Document.Deposit != nil {
		threshold, thresholdErr := event.Document.Deposit.Threshold(quotes[0].PriceMinor)
		if thresholdErr != nil {
			return AssignmentPreparation{}, thresholdErr
		}
		description += fmt.Sprintf(
			" Deposit minimum: %s %s. Final top-up deadline: %s.",
			minorAmount(threshold, rules.CurrencyScale),
			rules.Currency,
			rules.ClosesAt.In(zone).Format(time.RFC3339),
		)
	}
	expiresAt := now.Time.Add(assignmentPreparationTTL)
	if rules.ClosesAt.Before(expiresAt) {
		expiresAt = rules.ClosesAt
	}
	return AssignmentPreparation{
		Quotes:      quotes,
		ID:          uuid.New(),
		Arguments:   args,
		Description: description,
		ExpiresAt:   expiresAt.UTC(),
	}, nil
}

func minorAmount(amount int64, scale int32) string {
	if scale == 0 {
		return strconv.FormatInt(amount, 10)
	}
	divisor := int64(1)
	for range scale {
		divisor *= 10
	}
	return fmt.Sprintf("%d.%0*d", amount/divisor, scale, amount%divisor)
}

// ConfirmAssignment requires the same actor, operation, proposal and exact argument digest.
// A committed receipt remains replayable after expiry; new effects require an unexpired proposal.
func (s *Store) ConfirmAssignment(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	args PassAssignment,
	proposalID uuid.UUID,
) (registrations.Registration, error) {
	if proposalID == uuid.Nil {
		return registrations.Registration{}, ErrConflict
	}
	return s.changeRegistration(ctx, scope, actor, operation, args.change(), proposalID, nil)
}

// AssignmentOperation excludes owner joins and unprepared operator assignments.
func (s *Store) AssignmentOperation(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
) (registrations.Registration, error) {
	proposal, err := s.q.GetAssignmentPreparation(
		ctx,
		sqlc.GetAssignmentPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err != nil {
		return registrations.Registration{}, err
	}
	receipt, err := s.q.GetRegistrationOperation(
		ctx,
		sqlc.GetRegistrationOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    proposal.OperationID,
		},
	)
	if err != nil {
		return registrations.Registration{}, err
	}
	if !bytes.Equal(receipt.RequestHash, proposal.RequestHash) {
		return registrations.Registration{}, pgx.ErrNoRows
	}
	var result registrations.Registration
	err = json.Unmarshal(receipt.Result, &result)
	return result, err
}
