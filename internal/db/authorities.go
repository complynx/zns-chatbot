package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const (
	AuthorityNone           = "none"
	AuthorityAmbassador     = "ambassador"
	AuthorityAdministrator  = "administrator"
	authorityPageLimit      = 20
	authorityPreparationTTL = 5 * time.Minute
)

var ErrInvalidAuthority = errors.New("invalid event authority")

// EventAuthority is a local event role. Revocation preserves its version and audit history.
type EventAuthority struct {
	EventID uuid.UUID `json:"event_id"`
	UserID  uuid.UUID `json:"user_id"`
	Role    string    `json:"role"`
	Version int64     `json:"version"`
}
type AuthorityChange struct {
	EventID         uuid.UUID `json:"event_id"`
	UserID          uuid.UUID `json:"user_id"`
	Role            string    `json:"role"             enum:"none,ambassador,administrator"`
	ExpectedVersion int64     `json:"expected_version"                                      minimum:"0"`
}
type AuthorityPreparation struct {
	ID          uuid.UUID       `json:"proposal_id"`
	Arguments   AuthorityChange `json:"arguments"`
	Description string          `json:"description"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

func (c AuthorityChange) valid() bool {
	return c.EventID != uuid.Nil && c.UserID != uuid.Nil && c.ExpectedVersion >= 0 &&
		(c.Role == AuthorityNone || c.Role == AuthorityAmbassador || c.Role == AuthorityAdministrator)
}
func authorityRecord(row sqlc.ShuttleEventAuthority) EventAuthority {
	return EventAuthority{EventID: row.EventID, UserID: row.UserID, Role: row.Role, Version: row.Version}
}
func (s *Store) Authority(ctx context.Context, scope EventScope, event, user uuid.UUID) (EventAuthority, error) {
	// Check event scope even when no role has been assigned.
	if _, err := s.q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return EventAuthority{}, err
	}
	if _, err := s.q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: user, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return EventAuthority{}, err
	}
	row, err := s.q.ReadEventAuthority(
		ctx,
		sqlc.ReadEventAuthorityParams{
			EventID:        event,
			UserID:         user,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EventAuthority{EventID: event, UserID: user, Role: AuthorityNone}, nil
	}
	return authorityRecord(row), err
}

func (s *Store) Authorities(
	ctx context.Context,
	scope EventScope,
	event, after uuid.UUID,
	limit int32,
) ([]EventAuthority, error) {
	if limit < 1 || limit > authorityPageLimit {
		return nil, ErrInvalidAuthority
	}
	if _, err := s.q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return nil, err
	}
	rows, err := s.q.ListEventAuthorities(
		ctx,
		sqlc.ListEventAuthoritiesParams{
			EventID:    event,
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
			UserID:     after,
			Limit:      limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]EventAuthority, 0, len(rows))
	for _, row := range rows {
		result = append(result, authorityRecord(row))
	}
	return result, nil
}

// Authority changes and future payment confirmations take the event lock first.
// A completed revocation therefore precedes any later authority-protected effect.
func authorityTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	actor string,
	c AuthorityChange,
) error {
	if _, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: c.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return err
	}
	if _, err := q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: actor},
	); err != nil {
		return err
	}
	if _, err := q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: c.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return err
	}
	row, err := q.ReadEventAuthority(
		ctx,
		sqlc.ReadEventAuthorityParams{
			EventID:        c.EventID,
			UserID:         c.UserID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		row = sqlc.ShuttleEventAuthority{EventID: c.EventID, UserID: c.UserID, Role: AuthorityNone}
	} else if err != nil {
		return err
	}
	if row.Version != c.ExpectedVersion {
		return ErrConflict
	}
	return nil
}

func (s *Store) PrepareAuthority(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	c AuthorityChange,
) (AuthorityPreparation, error) {
	if !validAuthorityRequest(scope, actor, operation, c) {
		return AuthorityPreparation{}, ErrInvalidAuthority
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return AuthorityPreparation{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthorityPreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(ctx, authorityLock(scope, actor, operation)); err != nil {
		return AuthorityPreparation{}, err
	}
	prior, err := q.GetAuthorityPreparation(
		ctx,
		sqlc.GetAuthorityPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash[:]) {
			return AuthorityPreparation{}, ErrConflict
		}
		var result AuthorityPreparation
		err = json.Unmarshal(prior.Document, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AuthorityPreparation{}, err
	}
	if err = authorityTarget(ctx, q, scope, actor, c); err != nil {
		return AuthorityPreparation{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return AuthorityPreparation{}, err
	}
	result := AuthorityPreparation{
		ID:        uuid.New(),
		Arguments: c,
		ExpiresAt: now.Time.Add(authorityPreparationTTL),
		Description: fmt.Sprintf(
			"Set event %s authority for local user %s to %s (current version %d). This grants or revokes access to this event only.",
			c.EventID,
			c.UserID,
			c.Role,
			c.ExpectedVersion,
		),
	}
	doc, err := json.Marshal(result)
	if err != nil {
		return AuthorityPreparation{}, err
	}
	err = q.InsertAuthorityPreparation(
		ctx,
		sqlc.InsertAuthorityPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
			ProposalID:     result.ID,
			RequestHash:    hash[:],
			ExpiresAt:      pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
			Document:       doc,
		},
	)
	if err != nil {
		return AuthorityPreparation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthorityPreparation{}, err
	}
	return result, nil
}
func validAuthorityRequest(scope EventScope, actor, operation string, c AuthorityChange) bool {
	return scope.BotID > 0 && scope.Organization != "" && actor != "" && operation != "" && len(operation) <= 128 &&
		c.valid()
}
func authorityLock(scope EventScope, actor, operation string) string {
	return fmt.Sprintf("authority:%d:%s:%s:%s", scope.BotID, scope.Organization, actor, operation)
}

// ChangeAuthority requires global administrator authorization at the API boundary.
// Nonzero proposal binds native agent writes to an immutable exact-parameter approval.
func (s *Store) ChangeAuthority(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
	c AuthorityChange,
	proposal uuid.UUID,
) (EventAuthority, error) {
	if !validAuthorityRequest(scope, actor, operation, c) {
		return EventAuthority{}, ErrInvalidAuthority
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return EventAuthority{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return EventAuthority{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(ctx, authorityLock(scope, actor, operation)); err != nil {
		return EventAuthority{}, err
	}
	expires, err := authorityProposalExpiry(ctx, q, scope, actor, operation, proposal, hash[:])
	if err != nil {
		return EventAuthority{}, err
	}

	prior, err := q.GetAuthorityOperation(
		ctx,
		sqlc.GetAuthorityOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash[:]) {
			return EventAuthority{}, ErrConflict
		}
		var result EventAuthority
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return EventAuthority{}, err
	}
	if err = authorityTarget(ctx, q, scope, actor, c); err != nil {
		return EventAuthority{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return EventAuthority{}, err
	}
	if !expires.IsZero() && !now.Time.Before(expires) {
		return EventAuthority{}, ErrConflict
	}
	row, err := q.SetEventAuthority(
		ctx,
		sqlc.SetEventAuthorityParams{EventID: c.EventID, UserID: c.UserID, Role: c.Role},
	)
	if err != nil {
		return EventAuthority{}, err
	}
	result := authorityRecord(row)
	payload, err := json.Marshal(result)
	if err != nil {
		return EventAuthority{}, err
	}
	err = q.InsertAuthorityOperation(
		ctx,
		sqlc.InsertAuthorityOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
			RequestHash:    hash[:],
			Result:         payload,
		},
	)
	if err != nil {
		return EventAuthority{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return EventAuthority{}, err
	}
	return result, nil
}

func (s *Store) AuthorityOperation(
	ctx context.Context,
	scope EventScope,
	actor, operation string,
) (EventAuthority, error) {
	row, err := s.q.GetAuthorityOperation(
		ctx,
		sqlc.GetAuthorityOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err != nil {
		return EventAuthority{}, err
	}
	var result EventAuthority
	err = json.Unmarshal(row.Result, &result)
	return result, err
}

func authorityProposalExpiry(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	actor, operation string,
	proposal uuid.UUID,
	hash []byte,
) (time.Time, error) {
	if proposal == uuid.Nil {
		return time.Time{}, nil
	}
	prep, err := q.GetAuthorityPreparation(
		ctx,
		sqlc.GetAuthorityPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          actor,
			OperationID:    operation,
		},
	)
	if err != nil {
		return time.Time{}, err
	}
	if prep.ProposalID != proposal || !bytes.Equal(prep.RequestHash, hash) {
		return time.Time{}, ErrConflict
	}
	return prep.ExpiresAt.Time, nil
}
