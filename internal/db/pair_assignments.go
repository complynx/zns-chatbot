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

const pairAssign = "assign"

func (s *Store) HasPairAssignmentAuthority(ctx context.Context, scope PaymentScope) (bool, error) {
	return s.q.HasPairAssignmentAuthority(ctx, sqlc.HasPairAssignmentAuthorityParams{
		UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
		Subject: scope.Subject, GlobalAdmin: scope.GlobalAdmin,
	})
}

// PairAssignmentOperation returns the original committed result to a current administrator.
// A missing receipt does not prove that another controller has not committed it yet.
func (s *Store) PairAssignmentOperation(
	ctx context.Context,
	scope PaymentScope,
	operation string,
) (PairAssignmentResult, error) {
	if !paymentScopeValid(scope, operation) {
		return PairAssignmentResult{}, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	prep, err := q.GetPairPreparation(ctx, sqlc.GetPairPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: operation,
	})
	if err != nil {
		return PairAssignmentResult{}, err
	}
	var proposal PairAssignmentPreparation
	if err = json.Unmarshal(prep.Document, &proposal); err != nil {
		return PairAssignmentResult{}, err
	}
	eventID := proposal.Arguments.EventID
	if err = lockPairAssignment(ctx, q, scope, operation, eventID); err != nil {
		return PairAssignmentResult{}, err
	}
	row, err := q.GetPairOperation(ctx, sqlc.GetPairOperationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: prep.OperationID,
	})
	if err != nil {
		return PairAssignmentResult{}, err
	}
	if row.Kind != pairAssign || !bytes.Equal(row.RequestHash, prep.RequestHash) {
		return PairAssignmentResult{}, pgx.ErrNoRows
	}
	var result PairAssignmentResult
	if err = json.Unmarshal(row.Result, &result); err != nil {
		return PairAssignmentResult{}, err
	}
	if result.Pair.EventID != eventID {
		return PairAssignmentResult{}, pgx.ErrNoRows
	}
	return result, nil
}

type PairAssignment struct {
	EventID                 uuid.UUID `json:"event_id"`
	PairID                  uuid.UUID `json:"pair_id"`
	ExpectedEventVersion    int64     `json:"expected_event_version"`
	ExpectedPairVersion     int64     `json:"expected_pair_version"`
	ExpectedLeaderVersion   int64     `json:"expected_leader_version"`
	ExpectedFollowerVersion int64     `json:"expected_follower_version"`
}

type PairAssignmentPreparation struct {
	Timezone    string            `json:"timezone,omitempty"`
	Quotes      []AssignmentQuote `json:"quotes,omitempty"`
	ID          uuid.UUID         `json:"proposal_id"`
	Arguments   PairAssignment    `json:"arguments"`
	Description string            `json:"description"`
	ExpiresAt   time.Time         `json:"expires_at"`
}

// PairAssignmentResult contains both owner passes and is restricted to assignment administrators.
type PairAssignmentResult struct {
	Pair     registrations.Pair         `json:"pair"`
	Leader   registrations.Registration `json:"leader"`
	Follower registrations.Registration `json:"follower"`
}

func pairAssignmentValid(scope PaymentScope, operation string, args PairAssignment) bool {
	return paymentScopeValid(scope, operation) && args.EventID != uuid.Nil && args.PairID != uuid.Nil &&
		args.ExpectedEventVersion > 0 && args.ExpectedPairVersion > 0 &&
		args.ExpectedLeaderVersion > 0 && args.ExpectedFollowerVersion > 0
}

// lockPairAssignment serializes event effects and verifies current administrator access.
func lockPairAssignment(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	operation string,
	eventID uuid.UUID,
) error {
	if err := q.LockEventOperation(
		ctx,
		fmt.Sprintf("pair:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, operation),
	); err != nil {
		return err
	}
	return lockPairAssignmentAuthority(ctx, q, scope, eventID)
}

// The event row serializes role changes and assignment effects. It also permits
// historical receipt reads after sales close without rerunning effect eligibility.
func lockPairAssignmentAuthority(ctx context.Context, q *sqlc.Queries, scope PaymentScope, eventID uuid.UUID) error {
	if _, err := q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
		ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization,
	}); err != nil {
		return err
	}
	if err := lockPairAssignmentActor(ctx, q, scope); err != nil {
		return err
	}
	if scope.GlobalAdmin {
		return nil
	}
	authority, err := q.ReadEventAuthority(ctx, sqlc.ReadEventAuthorityParams{
		EventID: eventID, UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPairDenied
	}
	if err != nil {
		return err
	}
	if authority.Role != AuthorityAdministrator {
		return ErrPairDenied
	}
	return nil
}

func lockPairAssignmentActor(ctx context.Context, q *sqlc.Queries, scope PaymentScope) error {
	actor, err := q.LockAuthorityActor(ctx, sqlc.LockAuthorityActorParams{
		BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPairDenied
	}
	if err != nil {
		return err
	}
	if actor.ID != scope.UserID {
		return ErrPairDenied
	}
	return nil
}

type pairAssignmentCandidate struct {
	Quotes           []AssignmentQuote
	Event            events.Event
	Pair             sqlc.ShuttlePair
	Leader, Follower sqlc.ShuttleRegistration
	Now              pgtype.Timestamptz
}

func checkPairAssignment(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args PairAssignment,
	expiry pgtype.Timestamptz,
) (pairAssignmentCandidate, error) {
	var c pairAssignmentCandidate
	event, err := registrationEventForChange(ctx, q, scope.EventScope, args.EventID)
	if err != nil {
		return c, err
	}
	c.Event = event
	c.Pair, err = q.GetPair(ctx, sqlc.GetPairParams{ID: args.PairID, EventID: args.EventID})
	if err != nil {
		return c, err
	}
	if event.Version != args.ExpectedEventVersion || c.Pair.Version != args.ExpectedPairVersion ||
		c.Pair.State != registrations.Waitlisted {
		return c, ErrConflict
	}
	c.Leader, err = q.GetRegistrationByID(
		ctx,
		sqlc.GetRegistrationByIDParams{ID: c.Pair.LeaderRegistrationID, EventID: args.EventID},
	)
	if err != nil {
		return c, err
	}
	c.Follower, err = q.GetRegistrationByID(
		ctx,
		sqlc.GetRegistrationByIDParams{ID: c.Pair.FollowerRegistrationID, EventID: args.EventID},
	)
	if err != nil {
		return c, err
	}
	users := []uuid.UUID{c.Leader.UserID, c.Follower.UserID}
	slices.SortFunc(users, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, user := range users {
		if err = pairUser(ctx, q, scope, user); err != nil {
			return c, err
		}
	}
	for _, user := range users {
		if err = requireRegistrationDetails(ctx, q, event, user); err != nil {
			return c, err
		}
	}
	for _, member := range []struct {
		Entry   sqlc.ShuttleRegistration
		Role    string
		Version int64
	}{{c.Leader, registrations.Leader, args.ExpectedLeaderVersion}, {c.Follower, registrations.Follower, args.ExpectedFollowerVersion}} {
		if err = checkPairAssignmentMember(
			ctx,
			q,
			event,
			args.PairID,
			member.Entry,
			member.Role,
			member.Version,
		); err != nil {
			return c, err
		}
	}
	c.Now, err = registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, expiry)
	if err != nil {
		return c, err
	}
	prices, err := assignmentPrices(ctx, q, event, c.Now.Time, []string{registrations.Leader, registrations.Follower})
	if err != nil {
		return c, err
	}
	c.Quotes, err = assignmentQuotes(event, prices, c.Leader.ID, c.Follower.ID)
	return c, err
}

func checkPairAssignmentMember(
	ctx context.Context,
	q *sqlc.Queries,
	event events.Event,
	pairID uuid.UUID,
	entry sqlc.ShuttleRegistration,
	role string,
	version int64,
) error {
	if entry.State != registrations.Waitlisted || entry.Version != version ||
		entry.Role != role {
		return ErrConflict
	}
	matches, memberErr := q.PairMemberMatches(ctx, sqlc.PairMemberMatchesParams{
		PairID: pairID, EventID: event.ID, RegistrationID: entry.ID, Role: role,
	})
	if memberErr != nil {
		return memberErr
	}
	if !matches {
		return ErrConflict
	}
	count, countErr := q.CountAssignedRole(
		ctx,
		sqlc.CountAssignedRoleParams{EventID: event.ID, Role: role},
	)
	if countErr != nil {
		return countErr
	}
	capacity, capacityErr := event.Document.Registration.Capacity(role)
	if capacityErr != nil {
		return capacityErr
	}
	if count >= int64(capacity) {
		return registrations.ErrCapacity
	}
	return nil
}

// PreparePairAssignment freezes an exact proposal without reserving either role.
func (s *Store) PreparePairAssignment(
	ctx context.Context,
	scope PaymentScope,
	operation string,
	args PairAssignment,
) (PairAssignmentPreparation, error) {
	return s.preparePairAssignment(ctx, scope, operation, args, uuid.Nil)
}

func (s *Store) preparePairAssignment(ctx context.Context, scope PaymentScope, operation string,
	args PairAssignment, resume uuid.UUID,
) (PairAssignmentPreparation, error) {
	if !pairAssignmentValid(scope, operation, args) {
		return PairAssignmentPreparation{}, registrations.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockPairAssignment(ctx, q, scope, operation, args.EventID); err != nil {
		return PairAssignmentPreparation{}, err
	}
	prior, err := q.GetPairPreparation(
		ctx,
		sqlc.GetPairPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
		},
	)
	if err == nil {
		return replayPairAssignmentPreparation(ctx, q, prior, hash[:], resume)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PairAssignmentPreparation{}, err
	}
	if err = requireUnusedPairAssignmentOperation(ctx, q, scope, operation); err != nil {
		return PairAssignmentPreparation{}, err
	}
	if resume != uuid.Nil {
		result, adoptErr := adoptPairAssignment(ctx, q, scope, operation, resume, hash[:])
		if adoptErr != nil {
			return PairAssignmentPreparation{}, adoptErr
		}
		return result, tx.Commit(ctx)
	}
	c, err := checkPairAssignment(ctx, q, scope, args, pgtype.Timestamptz{})
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	expires := c.Now.Time.Add(assignmentPreparationTTL)
	if c.Event.Document.Registration.ClosesAt.Before(expires) {
		expires = c.Event.Document.Registration.ClosesAt
	}
	description, err := describePairAssignment(c, args)
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	result := PairAssignmentPreparation{
		Timezone:    c.Event.Document.Timezone,
		Quotes:      c.Quotes,
		ID:          uuid.New(),
		Arguments:   args,
		Description: description,
		ExpiresAt:   expires.UTC(),
	}
	document, err := json.Marshal(result)
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	err = q.InsertPairPreparation(
		ctx,
		sqlc.InsertPairPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
			ProposalID:     result.ID,
			RequestHash:    hash[:],
			ExpiresAt:      pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
			Document:       document,
		},
	)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

func describePairAssignment(c pairAssignmentCandidate, args PairAssignment) (string, error) {
	zone, err := time.LoadLocation(c.Event.Document.Timezone)
	if err != nil {
		return "", err
	}
	rules := c.Event.Document.Registration
	if rules.Pricing != nil {
		description := fmt.Sprintf(
			"Assign both passes atomically for %s (event %s, pair %s). Leader account %s: %s. Follower account %s: %s. Each payment window: %d seconds, capped at %s. No payment is recorded.",
			c.Event.Document.Name,
			args.EventID,
			args.PairID,
			c.Leader.UserID,
			describeAssignmentQuote(c.Quotes[0]),
			c.Follower.UserID,
			describeAssignmentQuote(c.Quotes[1]),
			rules.PaymentWindowSeconds,
			rules.ClosesAt.In(zone).Format(time.RFC3339),
		)
		return description, nil
	}
	description := fmt.Sprintf(
		"Assign both passes atomically for %s (event %s, pair %s). Leader account %s; follower account %s. Each price: %s %s. Each payment window: %d seconds, capped at %s. No payment is recorded.",
		c.Event.Document.Name,
		args.EventID,
		args.PairID,
		c.Leader.UserID,
		c.Follower.UserID,
		minorAmount(
			rules.PriceMinor,
			rules.CurrencyScale,
		),
		rules.Currency,
		rules.PaymentWindowSeconds,
		rules.ClosesAt.In(zone).Format(time.RFC3339),
	)
	if c.Event.Document.Deposit != nil {
		threshold, thresholdErr := c.Event.Document.Deposit.Threshold(rules.PriceMinor)
		if thresholdErr != nil {
			return "", thresholdErr
		}
		description += fmt.Sprintf(
			" Each deposit minimum: %s %s. Final top-up deadline: %s.",
			minorAmount(threshold, rules.CurrencyScale),
			rules.Currency,
			rules.ClosesAt.In(zone).Format(time.RFC3339),
		)
	}
	return description, nil
}

// ConfirmPairAssignment commits both passes and one immutable receipt in the same event transaction.
func (s *Store) ConfirmPairAssignment(
	ctx context.Context,
	scope PaymentScope,
	operation string,
	args PairAssignment,
	proposalID uuid.UUID,
) (PairAssignmentResult, error) {
	if !pairAssignmentValid(scope, operation, args) || proposalID == uuid.Nil {
		return PairAssignmentResult{}, registrations.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	prep, err := lockPairAssignmentConfirmation(ctx, q, scope, operation, args.EventID)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	operation = prep.OperationID
	if prep.ProposalID != proposalID || !bytes.Equal(prep.RequestHash, hash[:]) {
		return PairAssignmentResult{}, ErrConflict
	}
	prior, err := q.GetPairOperation(
		ctx,
		sqlc.GetPairOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
		},
	)
	if err == nil {
		if prior.Kind != pairAssign || !bytes.Equal(prior.RequestHash, hash[:]) {
			return PairAssignmentResult{}, ErrConflict
		}
		var result PairAssignmentResult
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PairAssignmentResult{}, err
	}
	c, err := checkPairAssignment(ctx, q, scope, args, prep.ExpiresAt)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	var approved PairAssignmentPreparation
	if err = json.Unmarshal(prep.Document, &approved); err != nil {
		return PairAssignmentResult{}, err
	}
	if err = checkAssignmentQuotes(c.Event, approved.Quotes, c.Quotes); err != nil {
		return PairAssignmentResult{}, err
	}
	result, err := writePairPasses(ctx, q, c)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	err = q.InsertPairOperation(
		ctx,
		sqlc.InsertPairOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
			Kind:           pairAssign,
			RequestHash:    hash[:],
			Result:         payload,
		},
	)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

// Both owner snapshots use the checked final instant inside the caller transaction.
func writePairPasses(ctx context.Context, q *sqlc.Queries, c pairAssignmentCandidate) (PairAssignmentResult, error) {
	leader, err := writeAssignedPass(ctx, q, c.Event, c.Leader, c.Now, c.Quotes[0])
	if err != nil {
		return PairAssignmentResult{}, err
	}
	follower, err := writeAssignedPass(ctx, q, c.Event, c.Follower, c.Now, c.Quotes[1])
	if err != nil {
		return PairAssignmentResult{}, err
	}
	pair, err := q.AssignPair(ctx, sqlc.AssignPairParams{ID: c.Pair.ID, Version: c.Pair.Version, AssignedAt: c.Now})
	if err != nil {
		return PairAssignmentResult{}, err
	}
	result := PairAssignmentResult{Pair: pairRecord(pair)}
	result.Leader, err = registrationRecord(ctx, q, leader)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	result.Follower, err = registrationRecord(ctx, q, follower)
	if err != nil {
		return PairAssignmentResult{}, err
	}
	return result, nil
}
