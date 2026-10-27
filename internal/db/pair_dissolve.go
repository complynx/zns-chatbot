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

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const pairDissolve = "dissolve"

// PairDissolutionOperation returns the original receipt only to its current active participant.
func (s *Store) PairDissolutionOperation(
	ctx context.Context,
	scope PaymentScope,
	operation string,
) (registrations.Pair, error) {
	if !paymentScopeValid(scope, operation) {
		return registrations.Pair{}, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrations.Pair{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.GetPairOperation(
		ctx,
		sqlc.GetPairOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
		},
	)
	if err != nil {
		return registrations.Pair{}, err
	}
	if row.Kind != pairDissolve {
		return registrations.Pair{}, pgx.ErrNoRows
	}
	var result registrations.Pair
	if err = json.Unmarshal(row.Result, &result); err != nil {
		return result, err
	}
	if _, err = lockParticipantPair(ctx, q, scope, result.EventID, result.ID, nil); err != nil {
		return registrations.Pair{}, err
	}
	return result, nil
}

type PairDissolution struct {
	EventID         uuid.UUID `json:"event_id"`
	PairID          uuid.UUID `json:"pair_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
}

// DissolvePair ends membership only. Owner passes, payment terms and money remain intact.
// Either active participant may end the relationship, including after sales close
// or module disablement. Administrator authority does not impersonate participant consent.
func (s *Store) DissolvePair(
	ctx context.Context,
	scope PaymentScope,
	operation string,
	args PairDissolution,
) (registrations.Pair, error) {
	return s.dissolvePair(ctx, scope, operation, args, nil)
}

func (s *Store) dissolvePair(
	ctx context.Context,
	scope PaymentScope,
	operation string,
	args PairDissolution,
	session *MiniAppPairSession,
) (registrations.Pair, error) {
	if !paymentScopeValid(scope, operation) || args.EventID == uuid.Nil || args.PairID == uuid.Nil ||
		args.ExpectedVersion < 1 {
		return registrations.Pair{}, registrations.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return registrations.Pair{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrations.Pair{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("pair:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, operation),
	); err != nil {
		return registrations.Pair{}, err
	}
	pair, err := lockParticipantPair(ctx, q, scope, args.EventID, args.PairID, session)
	if err != nil {
		return registrations.Pair{}, err
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
		if prior.Kind != pairDissolve || !bytes.Equal(prior.RequestHash, hash[:]) {
			return registrations.Pair{}, ErrConflict
		}
		var result registrations.Pair
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return registrations.Pair{}, err
	}
	if err = requireUnreservedPairParticipantOperation(ctx, q, scope, operation); err != nil {
		return registrations.Pair{}, err
	}
	if pair.Version != args.ExpectedVersion || pair.State == "dissolved" {
		return registrations.Pair{}, ErrConflict
	}
	result, err := dissolvePairMembership(ctx, q, pair)
	if err != nil {
		return registrations.Pair{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return registrations.Pair{}, err
	}
	err = q.InsertPairOperation(
		ctx,
		sqlc.InsertPairOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
			Kind:           pairDissolve,
			RequestHash:    hash[:],
			Result:         payload,
		},
	)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

func lockParticipantPair(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	eventID, pairID uuid.UUID,
	session *MiniAppPairSession,
) (sqlc.ShuttlePair, error) {
	if _, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return sqlc.ShuttlePair{}, err
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
		return sqlc.ShuttlePair{}, err
	}
	if actor.ID != scope.UserID {
		return sqlc.ShuttlePair{}, ErrPairDenied
	}
	if err = authorizePairMiniApp(ctx, q, actor, session); err != nil {
		return sqlc.ShuttlePair{}, err
	}
	pair, err := q.GetPair(ctx, sqlc.GetPairParams{ID: pairID, EventID: eventID})
	if err != nil {
		return pair, err
	}
	for _, registrationID := range []uuid.UUID{pair.LeaderRegistrationID, pair.FollowerRegistrationID} {
		entry, readErr := q.GetRegistrationByID(
			ctx,
			sqlc.GetRegistrationByIDParams{ID: registrationID, EventID: eventID},
		)
		if readErr != nil {
			return pair, readErr
		}
		if entry.UserID == scope.UserID {
			return pair, nil
		}
	}
	return pair, ErrPairDenied
}

func dissolvePairMembership(ctx context.Context, q *sqlc.Queries, pair sqlc.ShuttlePair) (registrations.Pair, error) {
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return registrations.Pair{}, err
	}
	row, err := q.DissolvePair(ctx, sqlc.DissolvePairParams{ID: pair.ID, Version: pair.Version, DissolvedAt: now})
	if err != nil {
		return registrations.Pair{}, err
	}
	removed, err := q.DeleteActivePairMembers(ctx, pair.ID)
	if err != nil {
		return registrations.Pair{}, err
	}
	if removed != pairMemberCount {
		return registrations.Pair{}, ErrConflict
	}
	for _, registrationID := range []uuid.UUID{pair.LeaderRegistrationID, pair.FollowerRegistrationID} {
		if _, err = q.AdvancePairRegistration(
			ctx,
			sqlc.AdvancePairRegistrationParams{ID: registrationID, EventID: pair.EventID},
		); err != nil {
			return registrations.Pair{}, err
		}
	}
	return pairRecord(row), nil
}
