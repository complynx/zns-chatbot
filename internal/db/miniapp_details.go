package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

// MutateMiniAppRegistrationDetails validates the session and saves the private
// field, redacted receipt, expectations, and manual action in one transaction.
// IAM must be checked before calling it. This operation never locks an event.
func (s *Store) MutateMiniAppRegistrationDetails(
	ctx context.Context, botID int64, bearer, generation [sha256.Size]byte,
	mutation registrationdetails.Mutation, protector *registrationdetails.Protector,
) (registrationdetails.Status, error) {
	m, err := mutation.Normalize()
	if err != nil {
		return registrationdetails.Status{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	attempt, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	request, err := json.Marshal(m)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	digest, err := protector.Fingerprint(attempt.User.ID, request)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	status, _, err := mutateRegistrationDetails(ctx, tx, attempt.User.ID, m, digest, protector)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_miniapp_details_actions(user_id,operation_id,field)
VALUES($1,$2,$3) ON CONFLICT(user_id,operation_id) DO NOTHING`, attempt.User.ID, m.OperationID, m.Field)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	return status, tx.Commit(ctx)
}

// MiniAppRegistrationDetails returns only presence/version, or an immutable
// historical receipt when operation is set. The owner comes from the session.
func (s *Store) MiniAppRegistrationDetails(
	ctx context.Context, botID int64, bearer, generation [sha256.Size]byte, operation uuid.UUID,
) (registrationdetails.Status, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	attempt, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	if operation == uuid.Nil {
		return registrationDetailsStatus(ctx, s.q.WithTx(tx), attempt.User.ID)
	}
	var raw []byte
	if err = tx.QueryRow(ctx, `SELECT result FROM shuttle_registration_details_operations WHERE user_id=$1 AND operation_id=$2`, attempt.User.ID, operation).
		Scan(&raw); err != nil {
		return registrationdetails.Status{}, err
	}
	var status registrationdetails.Status
	err = json.Unmarshal(raw, &status)
	return status, err
}
