package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

var ErrPairDenied = errors.New("pair participant access required")

const (
	pairInvite      = "invite"
	pairAccept      = "accept"
	pairDecline     = "decline"
	pairRevoke      = "revoke"
	pairPending     = "pending"
	pairExpired     = "expired"
	pairAccepted    = "accepted"
	pairMemberCount = 2
)

type PairInvitationRequest struct {
	EventID         uuid.UUID `json:"event_id"`
	TargetUserID    uuid.UUID `json:"target_user_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
}

type PairInvitationDecision struct {
	EventID         uuid.UUID `json:"event_id"`
	InvitationID    uuid.UUID `json:"invitation_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
	Role            string    `json:"role,omitempty"`
}

type PairResult struct {
	Invitation registrations.Invitation `json:"invitation"`
	Pair       *registrations.Pair      `json:"pair,omitempty"`
}

func pairInvitationRecord(row sqlc.ShuttlePairInvitation) registrations.Invitation {
	result := registrations.Invitation{
		ID:                    row.ID,
		EventID:               row.EventID,
		InviterRegistrationID: row.InviterRegistrationID,
		TargetUserID:          row.TargetUserID,
		State:                 row.State,
		Version:               row.Version,
		CreatedAt:             row.CreatedAt.Time.UTC(),
		ExpiresAt:             row.ExpiresAt.Time.UTC(),
	}
	if row.DecidedAt.Valid {
		at := row.DecidedAt.Time.UTC()
		result.DecidedAt = &at
	}
	if row.PairID.Valid {
		id := uuid.UUID(row.PairID.Bytes)
		result.PairID = &id
	}
	return result
}

func pairRecord(row sqlc.ShuttlePair) registrations.Pair {
	result := registrations.Pair{
		ID:                     row.ID,
		EventID:                row.EventID,
		LeaderRegistrationID:   row.LeaderRegistrationID,
		FollowerRegistrationID: row.FollowerRegistrationID,
		State:                  row.State,
		Version:                row.Version,
		QueuedAt:               row.QueuedAt.Time.UTC(),
		CreatedAt:              row.CreatedAt.Time.UTC(),
	}
	if row.AssignedAt.Valid {
		at := row.AssignedAt.Time.UTC()
		result.AssignedAt = &at
	}
	if row.DissolvedAt.Valid {
		at := row.DissolvedAt.Time.UTC()
		result.DissolvedAt = &at
	}
	return result
}

// pairOperation checks current actor access before replaying an immutable receipt.
// Callbacks run under the scoped event lock and must sample time after participant locks.
func (s *Store) pairOperation(
	ctx context.Context,
	scope PaymentScope,
	op, kind string,
	eventID uuid.UUID,
	args any,
	apply func(*sqlc.Queries) (PairResult, error),
	session *MiniAppPairSession,
) (PairResult, error) {
	if !paymentScopeValid(scope, op) || eventID == uuid.Nil {
		return PairResult{}, registrations.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return PairResult{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("pair:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, op),
	); err != nil {
		return PairResult{}, err
	}
	event, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return PairResult{}, err
	}
	actor, err := q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return PairResult{}, err
	}
	if actor.ID != scope.UserID {
		return PairResult{}, ErrPairDenied
	}
	if err = authorizePairMiniApp(ctx, q, actor, session); err != nil {
		return PairResult{}, err
	}
	prior, err := q.GetPairOperation(
		ctx,
		sqlc.GetPairOperationParams{
			BotID: scope.BotID, OrganizationID: scope.Organization,
			Actor: scope.Subject, OperationID: op,
		},
	)
	if err == nil {
		if prior.Kind != kind || !bytes.Equal(prior.RequestHash, hash[:]) {
			return PairResult{}, ErrConflict
		}
		var result PairResult
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PairResult{}, err
	}
	if !slices.Contains(event.Modules, "registrations") && kind != pairDecline && kind != pairRevoke {
		return PairResult{}, registrations.ErrUnavailable
	}
	result, err := apply(q)
	if err != nil {
		return PairResult{}, err
	}
	if err = enqueuePairInvitationNotice(ctx, q, result.Invitation.ID); err != nil {
		return PairResult{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return PairResult{}, err
	}
	err = q.InsertPairOperation(
		ctx,
		sqlc.InsertPairOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    op,
			Kind:           kind,
			RequestHash:    hash[:],
			Result:         payload,
		},
	)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

func pairUser(ctx context.Context, q *sqlc.Queries, scope PaymentScope, id uuid.UUID) error {
	_, err := q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: id, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	return err
}

func ensurePairUnheld(ctx context.Context, q *sqlc.Queries, id uuid.UUID) error {
	held, err := q.PairRegistrationHeld(ctx, id)
	if err != nil {
		return err
	}
	if !held.Valid || held.Bool {
		return ErrConflict
	}
	return nil
}

// expireOutgoing releases only an expired hold; a new invitation still requires current consent checks.
func expireOutgoing(
	ctx context.Context,
	q *sqlc.Queries,
	registration sqlc.ShuttleRegistration,
	now pgtype.Timestamptz,
) (sqlc.ShuttleRegistration, error) {
	prior, err := q.PendingOutgoingPairInvitation(ctx, registration.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return registration, nil
	}
	if err != nil {
		return registration, err
	}
	if now.Time.Before(prior.ExpiresAt.Time) {
		return registration, ErrConflict
	}
	if _, err = q.DecidePairInvitation(
		ctx,
		sqlc.DecidePairInvitationParams{ID: prior.ID, Version: prior.Version, State: pairExpired, DecidedAt: now},
	); err != nil {
		return registration, err
	}
	if err = enqueuePairInvitationNotice(ctx, q, prior.ID); err != nil {
		return registration, err
	}
	return q.AdvancePairRegistration(
		ctx,
		sqlc.AdvancePairRegistrationParams{ID: registration.ID, EventID: registration.EventID},
	)
}

func invitationTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	eventID, target uuid.UUID,
	role string,
) error {
	if err := pairUser(ctx, q, scope, target); err != nil {
		return err
	}
	row, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: eventID, UserID: target})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.State != registrations.Waitlisted || row.Role == role {
		return ErrConflict
	}
	return ensureNoActivePair(ctx, q, row.ID)
}

func ensureNoActivePair(ctx context.Context, q *sqlc.Queries, id uuid.UUID) error {
	active, err := q.HasActivePairMember(ctx, id)
	if err != nil {
		return err
	}
	if active {
		return ErrConflict
	}
	return nil
}

func (s *Store) CreatePairInvitation(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PairInvitationRequest,
) (PairResult, error) {
	return s.createPairInvitationOperation(ctx, scope, op, args, nil)
}

func (s *Store) createPairInvitationOperation(ctx context.Context, scope PaymentScope, op string,
	args PairInvitationRequest, session *MiniAppPairSession,
) (PairResult, error) {
	if args.TargetUserID == uuid.Nil || args.TargetUserID == scope.UserID || args.ExpectedVersion < 1 {
		return PairResult{}, registrations.ErrInvalid
	}
	return s.pairOperation(
		ctx,
		scope,
		op,
		pairInvite,
		args.EventID,
		args,
		func(q *sqlc.Queries) (PairResult, error) { return createPairInvitation(ctx, q, scope, args) },
		session,
	)
}

func createPairInvitation(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args PairInvitationRequest,
) (PairResult, error) {
	event, err := registrationEventForChange(ctx, q, scope.EventScope, args.EventID)
	if err != nil {
		return PairResult{}, err
	}
	inviter, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: args.EventID, UserID: scope.UserID})
	if err != nil {
		return PairResult{}, err
	}
	if inviter.State != registrations.Waitlisted || inviter.Version != args.ExpectedVersion {
		return PairResult{}, ErrConflict
	}
	if err = invitationTarget(ctx, q, scope, args.EventID, args.TargetUserID, inviter.Role); err != nil {
		return PairResult{}, err
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return PairResult{}, err
	}
	inviter, err = expireOutgoing(ctx, q, inviter, now)
	if err != nil {
		return PairResult{}, err
	}
	if err = ensurePairUnheld(ctx, q, inviter.ID); err != nil {
		return PairResult{}, err
	}
	row, err := q.InsertPairInvitation(
		ctx,
		sqlc.InsertPairInvitationParams{
			ID:                    uuid.New(),
			EventID:               args.EventID,
			InviterRegistrationID: inviter.ID,
			TargetUserID:          args.TargetUserID,
			CreatedAt:             now,
			ExpiresAt:             pgtype.Timestamptz{Time: event.Document.Registration.ClosesAt, Valid: true},
		},
	)
	if err != nil {
		return PairResult{}, err
	}
	_, err = q.AdvancePairRegistration(ctx, sqlc.AdvancePairRegistrationParams{ID: inviter.ID, EventID: args.EventID})
	return PairResult{Invitation: pairInvitationRecord(row)}, err
}

// DecidePairInvitation requires target consent for accept/decline and owner consent for revoke.
// An expired pending invitation is resolved as expired without creating a pair.
func (s *Store) DecidePairInvitation(
	ctx context.Context,
	scope PaymentScope,
	op, decision string,
	args PairInvitationDecision,
) (PairResult, error) {
	return s.decidePairInvitationOperation(ctx, scope, op, decision, args, nil)
}

func (s *Store) decidePairInvitationOperation(ctx context.Context, scope PaymentScope, op, decision string,
	args PairInvitationDecision, session *MiniAppPairSession,
) (PairResult, error) {
	if args.InvitationID == uuid.Nil || args.ExpectedVersion < 1 ||
		(decision != pairAccept && decision != pairDecline && decision != pairRevoke) ||
		(decision == pairAccept && !registrations.ValidRole(args.Role)) || (decision != pairAccept && args.Role != "") {
		return PairResult{}, registrations.ErrInvalid
	}
	return s.pairOperation(
		ctx,
		scope,
		op,
		decision,
		args.EventID,
		args,
		func(q *sqlc.Queries) (PairResult, error) { return decidePairInvitation(ctx, q, scope, decision, args) },
		session,
	)
}

func decidePairInvitation(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	decision string,
	args PairInvitationDecision,
) (PairResult, error) {
	invitation, err := q.GetPairInvitation(
		ctx,
		sqlc.GetPairInvitationParams{ID: args.InvitationID, EventID: args.EventID},
	)
	if err != nil {
		return PairResult{}, err
	}
	inviter, err := q.GetRegistrationByID(
		ctx,
		sqlc.GetRegistrationByIDParams{ID: invitation.InviterRegistrationID, EventID: args.EventID},
	)
	if err != nil {
		return PairResult{}, err
	}
	actorID := invitation.TargetUserID
	if decision == pairRevoke {
		actorID = inviter.UserID
	}
	if scope.UserID != actorID {
		return PairResult{}, ErrPairDenied
	}
	if invitation.State != pairPending || invitation.Version != args.ExpectedVersion {
		return PairResult{}, ErrConflict
	}
	if decision == pairAccept {
		if err = pairUser(ctx, q, scope, inviter.UserID); err != nil {
			return PairResult{}, err
		}
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return PairResult{}, err
	}
	var pair *registrations.Pair
	state := map[string]string{pairDecline: "declined", pairRevoke: "revoked", pairAccept: pairAccepted}[decision]
	if !now.Time.Before(invitation.ExpiresAt.Time) {
		state = pairExpired
	} else if decision == pairAccept {
		pair, err = acceptPair(ctx, q, scope, invitation, inviter, args.Role)
		if err != nil {
			return PairResult{}, err
		}
	}
	params := sqlc.DecidePairInvitationParams{
		ID:        invitation.ID,
		Version:   invitation.Version,
		State:     state,
		DecidedAt: now,
	}
	if pair != nil {
		params.PairID = pgtype.UUID{Bytes: pair.ID, Valid: true}
		params.DecidedAt = pgtype.Timestamptz{Time: pair.CreatedAt, Valid: true}
	}
	row, err := q.DecidePairInvitation(ctx, params)
	if err != nil {
		return PairResult{}, err
	}
	_, err = q.AdvancePairRegistration(ctx, sqlc.AdvancePairRegistrationParams{ID: inviter.ID, EventID: args.EventID})
	return PairResult{Invitation: pairInvitationRecord(row), Pair: pair}, err
}

func acceptPair(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	invitation sqlc.ShuttlePairInvitation,
	inviter sqlc.ShuttleRegistration,
	role string,
) (*registrations.Pair, error) {
	event, err := registrationEventForChange(ctx, q, scope.EventScope, invitation.EventID)
	if err != nil {
		return nil, err
	}
	if inviter.State != registrations.Waitlisted || inviter.Role == role {
		return nil, ErrConflict
	}
	if err = ensureNoActivePair(ctx, q, inviter.ID); err != nil {
		return nil, err
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, invitation.ExpiresAt)
	if err != nil {
		return nil, err
	}
	target, err := pairTargetRegistration(ctx, q, scope, event, role, now)
	if err != nil {
		return nil, err
	}
	leader, follower := inviter, target
	if leader.Role != registrations.Leader {
		leader, follower = follower, leader
	}
	queued := leader.RequestedAt
	if follower.RequestedAt.Time.After(queued.Time) {
		queued = follower.RequestedAt
	}
	row, err := q.InsertPair(
		ctx,
		sqlc.InsertPairParams{
			ID:                     uuid.New(),
			EventID:                invitation.EventID,
			LeaderRegistrationID:   leader.ID,
			FollowerRegistrationID: follower.ID,
			QueuedAt:               queued,
			CreatedAt:              now,
		},
	)
	if err != nil {
		return nil, err
	}
	count, err := q.InsertActivePairMembers(ctx, sqlc.InsertActivePairMembersParams{ID: row.ID, EventID: row.EventID})
	if err != nil {
		return nil, err
	}
	if count != pairMemberCount {
		return nil, ErrConflict
	}
	if _, err = q.AdvancePairRegistration(
		ctx,
		sqlc.AdvancePairRegistrationParams{ID: target.ID, EventID: row.EventID},
	); err != nil {
		return nil, err
	}
	result := pairRecord(row)
	return &result, nil
}

func pairTargetRegistration(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	event events.Event,
	role string,
	now pgtype.Timestamptz,
) (sqlc.ShuttleRegistration, error) {
	row, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: event.ID, UserID: scope.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		if err = requireRegistrationDetails(ctx, q, event, scope.UserID); err != nil {
			return row, err
		}
		return q.CreateRegistration(
			ctx,
			sqlc.CreateRegistrationParams{
				ID:          uuid.New(),
				EventID:     event.ID,
				UserID:      scope.UserID,
				Role:        role,
				RequestedAt: now,
			},
		)
	}
	if err != nil {
		return row, err
	}
	if row.State != registrations.Waitlisted || row.Role != role {
		return row, ErrConflict
	}
	row, err = expireOutgoing(ctx, q, row, now)
	if err != nil {
		return row, err
	}
	if err = ensurePairUnheld(ctx, q, row.ID); err != nil {
		return row, err
	}
	return row, nil
}

// PairInvitation is visible only to the inviter or the intended target.
func (s *Store) PairInvitation(
	ctx context.Context,
	scope PaymentScope,
	eventID, id uuid.UUID,
) (registrations.Invitation, error) {
	if !paymentScopeValid(scope, "read") {
		return registrations.Invitation{}, registrations.ErrInvalid
	}
	event, err := s.q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return registrations.Invitation{}, err
	}
	if !slices.Contains(event.Modules, "registrations") {
		return registrations.Invitation{}, registrations.ErrUnavailable
	}
	actor, err := s.q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return registrations.Invitation{}, err
	}
	if actor.ID != scope.UserID {
		return registrations.Invitation{}, ErrPairDenied
	}
	row, err := s.q.GetPairInvitation(ctx, sqlc.GetPairInvitationParams{ID: id, EventID: eventID})
	if err != nil {
		return registrations.Invitation{}, err
	}
	inviter, err := s.q.GetRegistrationByID(
		ctx,
		sqlc.GetRegistrationByIDParams{ID: row.InviterRegistrationID, EventID: eventID},
	)
	if err != nil {
		return registrations.Invitation{}, err
	}
	if row.TargetUserID != scope.UserID && inviter.UserID != scope.UserID {
		return registrations.Invitation{}, ErrPairDenied
	}
	return pairInvitationRecord(row), nil
}

// Used by both singleton preparation and execution so new holds cannot be bypassed.
func pairAssignmentAvailable(ctx context.Context, q *sqlc.Queries, id uuid.UUID) error {
	return ensurePairUnheld(ctx, q, id)
}
