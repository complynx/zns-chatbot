package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

var ErrUserRevocationInvalid = errors.New("invalid local user revocation")

const userRevocationPreparationTTL = 5 * time.Minute

type UserRevocationArguments struct {
	UserID          uuid.UUID `json:"user_id"`
	ExpectedSubject string    `json:"expected_subject" maxLength:"200"`
	ExpectedRevoked bool      `json:"expected_revoked"`
}

type UserRevocationPreparation struct {
	ID          uuid.UUID               `json:"proposal_id"`
	Arguments   UserRevocationArguments `json:"arguments"`
	Description string                  `json:"description"`
	ExpiresAt   time.Time               `json:"expires_at"`
}

// UserRevocationResult accompanying an error supplies trusted cache invalidation metadata.
// Only a nil error or subsequent receipt read proves that the transaction committed.
type UserRevocationResult struct {
	UserID  uuid.UUID `json:"user_id"`
	Subject string    `json:"subject"`
	Revoked bool      `json:"revoked"`
}

type UserRevocationDiagnostic struct {
	ProposalID  uuid.UUID             `json:"proposal_id"`
	Actor       string                `json:"actor"`
	OperationID string                `json:"operation_id"`
	Result      *UserRevocationResult `json:"result,omitempty"`
}

func userRevocationHash(args UserRevocationArguments) ([]byte, error) {
	if args.UserID == uuid.Nil || utf8.RuneCountInString(args.ExpectedSubject) > 200 {
		return nil, ErrUserRevocationInvalid
	}
	raw, err := json.Marshal(args)
	hash := sha256.Sum256(raw)
	return hash[:], err
}

func userRevocationKey(scope AdminScope, op string) sqlc.GetUserRevocationProposalParams {
	return sqlc.GetUserRevocationProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
	}
}

// Unlike beginAdminOperation, this leaves actor locking to the caller. Writers
// must lock actor and target together in UUID order, including self-revocation.
func (s *Store) beginUserRevocation(ctx context.Context, scope AdminScope, op string) (pgx.Tx, *sqlc.Queries, error) {
	if !adminScopeValid(scope, op) {
		return nil, nil, ErrAdminInvalid
	}
	if !scope.GlobalAdmin {
		return nil, nil, ErrAdminDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("user-revocation:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, op),
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, q, nil
}

func lockRevocationActor(ctx context.Context, q *sqlc.Queries, scope AdminScope) error {
	actor, err := q.LockRegistrationUser(ctx, sqlc.LockRegistrationUserParams{
		ID: scope.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization,
	})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && actor.ZitadelSubject != scope.Subject {
		return ErrAdminDenied
	}
	return err
}

func lockRevocationUsers(
	ctx context.Context,
	q *sqlc.Queries,
	scope AdminScope,
	target uuid.UUID,
) (sqlc.ShuttleUser, error) {
	rows, err := q.LockRevocationUsers(ctx, sqlc.LockRevocationUsersParams{
		Ids: []uuid.UUID{scope.UserID, target}, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return sqlc.ShuttleUser{}, err
	}
	var found sqlc.ShuttleUser
	activeActor := false
	for _, row := range rows {
		if row.ID == scope.UserID && row.ZitadelSubject == scope.Subject && !row.Revoked {
			activeActor = true
		}
		if row.ID == target {
			found = row
		}
	}
	if !activeActor {
		return sqlc.ShuttleUser{}, ErrAdminDenied
	}
	if found.ID == uuid.Nil {
		return found, pgx.ErrNoRows
	}
	return found, nil
}

func matchesRevocation(row sqlc.ShuttleUser, args UserRevocationArguments) bool {
	return row.ID == args.UserID && row.ZitadelSubject == args.ExpectedSubject && row.Revoked == args.ExpectedRevoked
}

// RevocationTarget reads only the deployment-scoped identity/state snapshot.
// The caller authorizes lookup. Cache recovery can also use this after self-revocation.
func (s *Store) RevocationTarget(
	ctx context.Context,
	botID int64,
	organization string,
	id uuid.UUID,
) (UserRevocationArguments, error) {
	row, err := s.q.GetRevocationUser(ctx, sqlc.GetRevocationUserParams{ID: id, BotID: botID, ZitadelOrg: organization})
	if err != nil {
		return UserRevocationArguments{}, err
	}
	return UserRevocationArguments{
		UserID:          row.ID,
		ExpectedSubject: row.ZitadelSubject,
		ExpectedRevoked: row.Revoked,
	}, nil
}

// PrepareUserRevocation stores exact observed identity and state for five minutes.
func (s *Store) PrepareUserRevocation(
	ctx context.Context,
	scope AdminScope,
	op string,
	args UserRevocationArguments,
) (UserRevocationPreparation, error) {
	hash, err := userRevocationHash(args)
	if err != nil {
		return UserRevocationPreparation{}, err
	}
	tx, q, err := s.beginUserRevocation(ctx, scope, op)
	if err != nil {
		return UserRevocationPreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetUserRevocationProposal(ctx, userRevocationKey(scope, op))
	if !errors.Is(err, pgx.ErrNoRows) {
		if authErr := lockRevocationActor(ctx, q, scope); authErr != nil {
			return UserRevocationPreparation{}, authErr
		}
		if err != nil {
			return UserRevocationPreparation{}, err
		}
		if !bytes.Equal(prior.RequestHash, hash) {
			return UserRevocationPreparation{}, ErrConflict
		}
		var prep UserRevocationPreparation
		err = json.Unmarshal(prior.Document, &prep)
		return prep, err
	}
	target, err := lockRevocationUsers(ctx, q, scope, args.UserID)
	if err != nil {
		return UserRevocationPreparation{}, err
	}
	if !matchesRevocation(target, args) {
		return UserRevocationPreparation{}, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return UserRevocationPreparation{}, err
	}
	description := fmt.Sprintf(
		"Revoke local access for account %s, IAM subject %q, currently revoked=%t. This "+
			"records an audit even if already revoked. Remote IAM roles are unchanged.",
		target.ID,
		target.ZitadelSubject,
		target.Revoked,
	)
	if target.ID == scope.UserID {
		description += " This is your own account: further local API calls, including receipt recovery, will " +
			"be denied. Another active global administrator can read the original receipt."
	}
	prep := UserRevocationPreparation{
		ID:          uuid.New(),
		Arguments:   args,
		Description: description,
		ExpiresAt:   now.Time.Add(userRevocationPreparationTTL),
	}
	document, err := json.Marshal(prep)
	if err != nil {
		return UserRevocationPreparation{}, err
	}
	err = q.InsertUserRevocationProposal(ctx, sqlc.InsertUserRevocationProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: op,
		ProposalID: prep.ID, TargetID: target.ID, RequestHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: prep.ExpiresAt, Valid: true}, Document: document,
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return prep, err
}

// ConfirmUserRevocation commits the revoke, audit and original receipt together.
func (s *Store) ConfirmUserRevocation(
	ctx context.Context,
	scope AdminScope,
	op string,
	args UserRevocationArguments,
	proposal uuid.UUID,
) (UserRevocationResult, error) {
	hash, err := userRevocationHash(args)
	if err != nil || proposal == uuid.Nil {
		return UserRevocationResult{}, ErrUserRevocationInvalid
	}
	tx, q, err := s.beginUserRevocation(ctx, scope, op)
	if err != nil {
		return UserRevocationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetUserRevocationProposal(ctx, userRevocationKey(scope, op))
	if err != nil || prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) || len(prior.Result) != 0 {
		if authErr := lockRevocationActor(ctx, q, scope); authErr != nil {
			return UserRevocationResult{}, authErr
		}
		if err != nil {
			return UserRevocationResult{}, err
		}
		return savedRevocationResult(prior, proposal, hash)
	}
	target, err := lockRevocationUsers(ctx, q, scope, args.UserID)
	if err != nil {
		return UserRevocationResult{}, err
	}
	if !matchesRevocation(target, args) {
		return UserRevocationResult{}, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return UserRevocationResult{}, err
	}
	if !now.Time.Before(prior.ExpiresAt.Time) {
		return UserRevocationResult{}, ErrConflict
	}
	u, err := revokeUserTx(ctx, q, target.ID, scope.Subject)
	if err != nil {
		return UserRevocationResult{}, err
	}
	result := UserRevocationResult{UserID: u.ID, Subject: u.Subject, Revoked: true}
	receipt, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	saved, err := q.SaveUserRevocationResult(ctx, sqlc.SaveUserRevocationResultParams{
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

func savedRevocationResult(
	prior sqlc.ShuttleUserRevocationProposal,
	proposal uuid.UUID,
	hash []byte,
) (UserRevocationResult, error) {
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return UserRevocationResult{}, ErrConflict
	}
	var result UserRevocationResult
	err := json.Unmarshal(prior.Result, &result)
	return result, err
}

// RevokeScopedUser preserves the operator's repeated-audit and self-revoke behavior.
func (s *Store) RevokeScopedUser(ctx context.Context, scope AdminScope, target uuid.UUID) (User, error) {
	if target == uuid.Nil {
		return User{}, ErrUserRevocationInvalid
	}
	tx, q, err := s.beginUserRevocation(ctx, scope, "operator:"+target.String())
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockRevocationUsers(ctx, q, scope, target); err != nil {
		return User{}, err
	}
	u, err := revokeUserTx(ctx, q, target, scope.Subject)
	if err != nil {
		return User{}, err
	}
	return u, tx.Commit(ctx)
}

// UserRevocationReceipt returns only this active administrator's original receipt.
func (s *Store) UserRevocationReceipt(ctx context.Context, scope AdminScope, op string) (*UserRevocationResult, error) {
	tx, q, err := s.beginUserRevocation(ctx, scope, op)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockRevocationActor(ctx, q, scope); err != nil {
		return nil, err
	}
	row, err := q.GetUserRevocationProposal(ctx, userRevocationKey(scope, op))
	if err != nil {
		return nil, err
	}
	if len(row.Result) == 0 {
		return nil, pgx.ErrNoRows
	}
	var result UserRevocationResult
	err = json.Unmarshal(row.Result, &result)
	return &result, err
}

// UserRevocationDiagnostic is read-only and cannot confirm another actor's proposal.
// It takes no operation lock: waiting for a writer while holding its target's
// actor share lock would deadlock. A missing result means pending in this snapshot.
func (s *Store) UserRevocationDiagnostic(
	ctx context.Context,
	scope AdminScope,
	proposal uuid.UUID,
) (UserRevocationDiagnostic, error) {
	if !adminScopeValid(scope, "diagnostic") || proposal == uuid.Nil {
		return UserRevocationDiagnostic{}, ErrAdminInvalid
	}
	if !scope.GlobalAdmin {
		return UserRevocationDiagnostic{}, ErrAdminDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserRevocationDiagnostic{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockRevocationActor(ctx, q, scope); err != nil {
		return UserRevocationDiagnostic{}, err
	}
	row, err := q.GetUserRevocationProposalByID(ctx, sqlc.GetUserRevocationProposalByIDParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, ProposalID: proposal,
	})
	if err != nil {
		return UserRevocationDiagnostic{}, err
	}
	result := UserRevocationDiagnostic{ProposalID: row.ProposalID, Actor: row.Actor, OperationID: row.OperationID}
	if len(row.Result) != 0 {
		result.Result = new(UserRevocationResult)
		err = json.Unmarshal(row.Result, result.Result)
	}
	return result, err
}
