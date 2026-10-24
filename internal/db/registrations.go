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
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const registrationActionRegister = "register"

// RegistrationChange identifies either a user's solo request or an administrator's assignment.
// Assignment must carry both versions shown to the administrator.
type RegistrationChange struct {
	Kind                 string    `json:"kind"`
	EventID              uuid.UUID `json:"event_id"`
	UserID               uuid.UUID `json:"user_id"`
	RegistrationID       uuid.UUID `json:"registration_id"`
	Role                 string    `json:"role"`
	ExpectedEventVersion int64     `json:"expected_event_version"`
	ExpectedVersion      int64     `json:"expected_version"`
}

func validRegistrationChange(c RegistrationChange) bool {
	if c.EventID == uuid.Nil {
		return false
	}
	switch c.Kind {
	case registrationActionRegister:
		return c.UserID != uuid.Nil && registrations.ValidRole(c.Role) && c.RegistrationID == uuid.Nil &&
			c.ExpectedVersion == 0 &&
			c.ExpectedEventVersion == 0
	case "assign":
		return c.RegistrationID != uuid.Nil && c.UserID == uuid.Nil && c.Role == "" && c.ExpectedVersion > 0 &&
			c.ExpectedEventVersion > 0
	default:
		return false
	}
}

// ChangeRegistration locks the event before checking capacity and configuration.
// Registration, pass snapshot and immutable receipt commit in the same transaction.
// Callers enforce administrator rights for assignment; user requests derive their local account.
func (s *Store) ChangeRegistration(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	change RegistrationChange,
) (registrations.Registration, error) {
	return s.changeRegistration(ctx, scope, actor, operation, change, uuid.Nil, nil, nil)
}

func (s *Store) changeRegistration(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	change RegistrationChange,
	proposalID uuid.UUID,
	selection *RegistrationEntry,
	session *MiniAppAssignmentSession,
) (registrations.Registration, error) {
	if scope.BotID <= 0 || scope.Organization == "" || actor == "" || operation == "" || len(operation) > 128 ||
		!validRegistrationChange(change) {
		return registrations.Registration{}, registrations.ErrInvalid
	}
	request, err := json.Marshal(change)
	if err != nil {
		return registrations.Registration{}, err
	}
	digest := sha256.Sum256(request)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrations.Registration{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("registration:%d:%s:%s:%s", scope.BotID, scope.Organization, actor, operation),
	); err != nil {
		return registrations.Registration{}, err
	}
	transport := operation
	operation, err = assignmentOperationForChange(ctx, q, scope, actor, operation, change.EventID, proposalID, session)
	if err != nil {
		return registrations.Registration{}, err
	}

	expiresAt, err := assignmentConfirmationExpiry(ctx, q, scope, actor, operation, proposalID, digest[:])
	if err != nil {
		return registrations.Registration{}, err
	}
	receipt, err := q.GetRegistrationOperation(
		ctx,
		sqlc.GetRegistrationOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err == nil {
		result, decodeErr := decodeRegistrationReceipt(receipt, digest[:])
		if decodeErr != nil {
			return registrations.Registration{}, decodeErr
		}
		return finishAssignmentConfirmation(ctx, tx, q, scope, actor, transport, proposalID, result, session)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return registrations.Registration{}, err
	}
	if err = nativeDetailsOutcome(ctx, tx, q, scope, change, selection, false); err != nil {
		return registrations.Registration{}, err
	}
	approved, err := assignmentApprovedQuotes(ctx, q, scope, actor, operation, proposalID)
	if err != nil {
		return registrations.Registration{}, err
	}
	row, err := applyRegistrationChange(ctx, q, scope, actor, change, expiresAt, approved, selection)
	if err != nil {
		return registrations.Registration{}, nativeDetailsFailure(ctx, tx, q, scope, change, selection, err)
	}

	result, err := registrationRecord(ctx, q, row)
	if err != nil {
		return registrations.Registration{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return registrations.Registration{}, err
	}
	if err = q.InsertRegistrationOperation(
		ctx,
		sqlc.InsertRegistrationOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
			RequestHash:    digest[:],
			Result:         payload,
		},
	); err != nil {
		return registrations.Registration{}, err
	}
	return finishAssignmentConfirmation(ctx, tx, q, scope, actor, transport, proposalID, result, session)
}

func decodeRegistrationReceipt(
	receipt sqlc.ShuttleRegistrationOperation,
	digest []byte,
) (registrations.Registration, error) {
	if !bytes.Equal(receipt.RequestHash, digest) {
		return registrations.Registration{}, ErrConflict
	}
	var result registrations.Registration
	err := json.Unmarshal(receipt.Result, &result)
	return result, err
}

func registerSolo(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	actor string,
	c RegistrationChange,
	event events.Event,
	selection *RegistrationEntry,
) (sqlc.ShuttleRegistration, error) {
	user, err := q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: c.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	if err != nil {
		return sqlc.ShuttleRegistration{}, err
	}
	if user.ZitadelSubject != actor {
		return sqlc.ShuttleRegistration{}, registrations.ErrUnavailable
	}
	var selectionExpiry pgtype.Timestamptz
	if selection != nil {
		selectionExpiry = pgtype.Timestamptz{Time: selection.ExpiresAt, Valid: true}
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, selectionExpiry)
	if err != nil {
		return sqlc.ShuttleRegistration{}, err
	}
	row, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: c.EventID, UserID: c.UserID})
	if err == nil {
		if row.Role != c.Role {
			return row, ErrConflict
		}
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return row, err
	}
	if err = requireRegistrationDetails(ctx, q, event, c.UserID); err != nil {
		return row, err
	}
	return q.CreateRegistration(
		ctx,
		sqlc.CreateRegistrationParams{
			ID:          uuid.New(),
			EventID:     c.EventID,
			UserID:      c.UserID,
			Role:        c.Role,
			RequestedAt: now,
		},
	)
}

func assignPass(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	event events.Event,
	c RegistrationChange,
	expiresAt pgtype.Timestamptz,
	approved []AssignmentQuote,
) (sqlc.ShuttleRegistration, error) {
	row, err := q.GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: c.RegistrationID, EventID: c.EventID})
	if err != nil {
		return row, err
	}
	if _, err = q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: row.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return row, err
	}
	if err = pairAssignmentAvailable(ctx, q, row.ID); err != nil {
		return row, err
	}
	if row.Version != c.ExpectedVersion || row.State != registrations.Waitlisted {
		return row, ErrConflict
	}
	count, err := q.CountAssignedRole(ctx, sqlc.CountAssignedRoleParams{EventID: c.EventID, Role: row.Role})
	if err != nil {
		return row, err
	}
	capacity, err := event.Document.Registration.Capacity(row.Role)
	if err != nil {
		return row, err
	}
	if count >= int64(capacity) {
		return row, registrations.ErrCapacity
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, expiresAt)
	if err != nil {
		return row, err
	}
	prices, err := assignmentPrices(ctx, q, event, now.Time, []string{row.Role})
	if err != nil {
		return row, err
	}
	quotes, err := assignmentQuotes(event, prices, row.ID)
	if err != nil {
		return row, err
	}
	if err = checkAssignmentQuotes(event, approved, quotes); err != nil {
		return row, err
	}
	return writeAssignedPass(ctx, q, event, row, now, quotes[0])
}

// writeAssignedPass freezes one owner pass after the caller checks eligibility and capacity.
// The caller owns the event transaction and the final assignment instant.
func writeAssignedPass(
	ctx context.Context,
	q *sqlc.Queries,
	event events.Event,
	row sqlc.ShuttleRegistration,
	now pgtype.Timestamptz,
	quote AssignmentQuote,
) (sqlc.ShuttleRegistration, error) {
	if err := requireRegistrationDetails(ctx, q, event, row.UserID); err != nil {
		return row, err
	}
	row, err := q.AssignRegistration(ctx, sqlc.AssignRegistrationParams{ID: row.ID, Version: row.Version})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrConflict
	}
	if err != nil {
		return row, err
	}
	rules := event.Document.Registration
	pass, err := q.CreatePass(
		ctx,
		sqlc.CreatePassParams{
			ID:             uuid.New(),
			RegistrationID: row.ID,
			PriceMinor:     quote.PriceMinor,
			Currency:       rules.Currency,
			CurrencyScale:  rules.CurrencyScale,
			AssignedAt:     now,
			PaymentDueAt:   pgtype.Timestamptz{Time: rules.PaymentDueAt(now.Time), Valid: true},
		},
	)
	if err != nil {
		return row, err
	}
	if err = writePassTier(
		ctx,
		q,
		pass.ID,
		registrations.PriceQuote{PriceMinor: quote.PriceMinor, Tier: quote.Tier},
	); err != nil {
		return row, err
	}
	if quote.Deposit != nil {
		raw, e := json.Marshal(quote.Deposit)
		if e != nil {
			return row, e
		}
		err = q.CreatePassDepositPolicy(ctx, sqlc.CreatePassDepositPolicyParams{PassID: pass.ID, Document: raw})
	}
	return row, err
}

func registrationRecord(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ShuttleRegistration,
) (registrations.Registration, error) {
	result := registrations.Registration{
		ID:          row.ID,
		EventID:     row.EventID,
		UserID:      row.UserID,
		Role:        row.Role,
		State:       row.State,
		Version:     row.Version,
		RequestedAt: row.RequestedAt.Time.UTC(),
	}
	if row.State != registrations.Assigned {
		return result, nil
	}
	pass, err := q.GetPass(ctx, row.ID)
	if err != nil {
		return registrations.Registration{}, err
	}
	result.Pass = &registrations.Pass{
		ID:             pass.ID,
		RegistrationID: pass.RegistrationID,
		PriceMinor:     pass.PriceMinor,
		Currency:       pass.Currency,
		CurrencyScale:  pass.CurrencyScale,
		AssignedAt:     pass.AssignedAt.Time.UTC(),
		PaymentDueAt:   pass.PaymentDueAt.Time.UTC(),
	}
	result.Pass.Tier, err = readPassTier(ctx, q, pass.ID)
	if err != nil {
		return registrations.Registration{}, err
	}
	raw, err := q.ReadPassDepositPolicy(ctx, pass.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return registrations.Registration{}, err
	}
	if err == nil {
		var snapshot registrations.DepositSnapshot
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			return registrations.Registration{}, err
		}
		result.Pass.Deposit = &snapshot
	}
	return result, nil
}

// Registration returns only the specified owner's registration within this deployment.
func (s *Store) Registration(
	ctx context.Context,
	scope EventScope,
	eventID, userID uuid.UUID,
) (registrations.Registration, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return registrations.Registration{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err = q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return registrations.Registration{}, err
	}
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return registrations.Registration{}, err
	}
	if user.BotID != scope.BotID || user.ZitadelOrg != scope.Organization || user.Revoked {
		return registrations.Registration{}, pgx.ErrNoRows
	}
	row, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: eventID, UserID: userID})
	if err != nil {
		return registrations.Registration{}, err
	}
	return registrationRecord(ctx, q, row)
}

func (s *Store) RegistrationOperation(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
) (registrations.Registration, error) {
	receipt, err := s.q.GetRegistrationOperation(
		ctx,
		sqlc.GetRegistrationOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err != nil {
		return registrations.Registration{}, err
	}
	var result registrations.Registration
	err = json.Unmarshal(receipt.Result, &result)
	return result, err
}

// EventRegistrations pages the event queue for an authorized operator.
func (s *Store) EventRegistrations(
	ctx context.Context,
	scope EventScope,
	eventID, after uuid.UUID,
	limit int32,
) ([]registrations.Registration, error) {
	if limit < 1 || limit > 100 {
		return nil, registrations.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err = q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return nil, err
	}
	rows, err := q.ListEventRegistrations(
		ctx,
		sqlc.ListEventRegistrationsParams{
			EventID:    eventID,
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
			ID:         after,
			Limit:      limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]registrations.Registration, 0, len(rows))
	for _, row := range rows {
		entry, decodeErr := registrationRecord(ctx, q, row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, entry)
	}
	return result, nil
}

// ModuleAvailable requires an assigned pass, including unpaid passes, and event enablement.
// Payment deadlines do not revoke this entitlement. A consuming mutation must recheck it.
func (s *Store) ModuleAvailable(
	ctx context.Context,
	scope EventScope,
	eventID, userID uuid.UUID,
	module string,
) (bool, error) {
	return s.q.UserModuleAvailable(
		ctx,
		sqlc.UserModuleAvailableParams{
			ID:             eventID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ID_2:           userID,
			Column5:        module,
		},
	)
}

func applyRegistrationChange(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	actor string,
	change RegistrationChange,
	expiresAt pgtype.Timestamptz,
	approved []AssignmentQuote,
	selection *RegistrationEntry,
) (sqlc.ShuttleRegistration, error) {
	event, err := registrationEventForChange(ctx, q, scope, change.EventID)
	if err != nil {
		return sqlc.ShuttleRegistration{}, err
	}
	var row sqlc.ShuttleRegistration
	if change.Kind == registrationActionRegister {
		if selection != nil && event.Version != selection.EventVersion {
			return row, ErrConflict
		}
		row, err = registerSolo(ctx, q, scope, actor, change, event, selection)
	} else {
		if event.Version != change.ExpectedEventVersion {
			return sqlc.ShuttleRegistration{}, ErrConflict
		}
		row, err = assignPass(ctx, q, scope, event, change, expiresAt, approved)
	}
	if err != nil {
		return sqlc.ShuttleRegistration{}, err
	}
	return row, nil
}

func registrationEventForChange(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	eventID uuid.UUID,
) (events.Event, error) {
	eventRow, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return events.Event{}, err
	}
	raw, err := q.ReadRegistrationRules(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, registrations.ErrUnavailable
	}
	if err != nil {
		return events.Event{}, err
	}
	deposit, err := q.ReadEventDepositPolicy(ctx, eventID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, err
	}
	event, err := eventRecord(eventRow, raw, deposit)
	if err != nil {
		return events.Event{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return events.Event{}, err
	}
	if !slices.Contains(event.Document.Modules, "registrations") || event.Document.Registration == nil ||
		!now.Time.Before(event.Document.Registration.ClosesAt) {
		return events.Event{}, registrations.ErrUnavailable
	}
	return event, nil
}

// RegistrationJoinOperation excludes receipts for other registration actions.
// The canonical solo request can be reconstructed from its immutable result.
func (s *Store) RegistrationJoinOperation(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
) (registrations.Registration, error) {
	receipt, err := s.q.GetRegistrationOperation(
		ctx,
		sqlc.GetRegistrationOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err != nil {
		return registrations.Registration{}, err
	}
	var result registrations.Registration
	if err = json.Unmarshal(receipt.Result, &result); err != nil {
		return result, err
	}
	request, err := json.Marshal(
		RegistrationChange{
			Kind:    registrationActionRegister,
			EventID: result.EventID,
			UserID:  result.UserID,
			Role:    result.Role,
		},
	)
	if err != nil {
		return registrations.Registration{}, err
	}
	digest := sha256.Sum256(request)
	if !bytes.Equal(receipt.RequestHash, digest[:]) {
		return registrations.Registration{}, pgx.ErrNoRows
	}
	return result, nil
}

func assignmentConfirmationExpiry(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	actor, operation string,
	proposalID uuid.UUID,
	digest []byte,
) (pgtype.Timestamptz, error) {
	if proposalID == uuid.Nil {
		return pgtype.Timestamptz{}, nil
	}
	proposal, err := q.GetAssignmentPreparation(
		ctx,
		sqlc.GetAssignmentPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.Timestamptz{}, ErrConflict
	}
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	if proposal.ProposalID != proposalID || !bytes.Equal(proposal.RequestHash, digest) {
		return pgtype.Timestamptz{}, ErrConflict
	}
	return proposal.ExpiresAt, nil
}

// registrationMutationTime samples the database clock after the recipient lock.
// Never carry a pre-lock timestamp into a new registration or assignment effect.
func registrationMutationTime(
	ctx context.Context,
	q *sqlc.Queries,
	closesAt time.Time,
	expiresAt pgtype.Timestamptz,
) (pgtype.Timestamptz, error) {
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return pgtype.Timestamptz{}, err
	}
	if !now.Time.Before(closesAt) {
		return pgtype.Timestamptz{}, registrations.ErrUnavailable
	}
	if expiresAt.Valid && !now.Time.Before(expiresAt.Time) {
		return pgtype.Timestamptz{}, ErrConflict
	}
	return now, nil
}
