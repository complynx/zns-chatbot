package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

// RegistrationDetailsStatus never returns the private legal data.
func (s *Store) RegistrationDetailsStatus(ctx context.Context, scope ProfileScope) (registrationdetails.Status, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return registrationdetails.Status{}, err
	}
	return registrationDetailsStatus(ctx, s.q.WithTx(tx), scope.UserID)
}

func registrationDetailsStatus(
	ctx context.Context,
	q *sqlc.Queries,
	userID uuid.UUID,
) (registrationdetails.Status, error) {
	row, err := q.GetRegistrationDetails(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return registrationdetails.Status{}, nil
	}
	return registrationdetails.Status{
		Version:               row.Version,
		LegalNamePresent:      row.LegalNamePresent,
		PassportNumberPresent: row.PassportNumberPresent,
	}, err
}

// MutateRegistrationDetails serializes owner changes and records only redacted
// immutable receipts. Commit uncertainty is recovered with the same operation.
func (s *Store) MutateRegistrationDetails(
	ctx context.Context,
	scope ProfileScope,
	mutation registrationdetails.Mutation,
	protector *registrationdetails.Protector,
) (registrationdetails.Status, error) {
	m, err := mutation.Normalize()
	if err != nil {
		return registrationdetails.Status{}, err
	}
	request, err := json.Marshal(m)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	digest, err := protector.Fingerprint(scope.UserID, request)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, true); err != nil {
		return registrationdetails.Status{}, err
	}
	status, replayed, err := mutateRegistrationDetails(ctx, tx, scope.UserID, m, digest, protector)
	if err != nil || replayed {
		return status, err
	}
	return status, tx.Commit(ctx)
}

// mutateRegistrationDetails applies a normalized mutation and its fingerprint.
// The caller holds the owner's UPDATE lock and owns the transaction. A replay
// returns its historical receipt without resolving newer expectations.
func mutateRegistrationDetails(
	ctx context.Context,
	tx pgx.Tx,
	owner uuid.UUID,
	m registrationdetails.Mutation,
	digest []byte,
	protector *registrationdetails.Protector,
) (registrationdetails.Status, bool, error) {
	prior, err := replayRegistrationDetails(ctx, tx, owner, m.OperationID, digest)
	if !errors.Is(err, pgx.ErrNoRows) {
		return prior, true, err
	}
	current, status, err := readRegistrationDetails(ctx, sqlc.New(tx), owner, protector)
	if err != nil {
		return status, false, err
	}
	if status.Version != m.ExpectedVersion {
		return registrationdetails.Status{}, false, ErrConflict
	}
	updated, err := current.Apply(m)
	if err != nil {
		return registrationdetails.Status{}, false, err
	}
	if updated != current {
		status = updated.Status(status.Version + 1)
		if err = saveRegistrationDetails(ctx, tx, owner, updated, status, protector); err != nil {
			return registrationdetails.Status{}, false, err
		}
	}
	result, err := json.Marshal(status)
	if err != nil {
		return registrationdetails.Status{}, false, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_registration_details_operations(user_id,operation_id,request_hash,result) VALUES($1,$2,$3,$4)`,
		owner,
		m.OperationID,
		digest,
		result,
	)
	if err != nil {
		return registrationdetails.Status{}, false, err
	}
	if err = resolveDetailsRequests(ctx, tx, owner, status); err != nil {
		return registrationdetails.Status{}, false, err
	}
	return status, false, nil
}

func readRegistrationDetails(
	ctx context.Context,
	q *sqlc.Queries,
	owner uuid.UUID,
	protector *registrationdetails.Protector,
) (registrationdetails.Record, registrationdetails.Status, error) {
	row, err := q.GetRegistrationDetails(ctx, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return registrationdetails.Record{}, registrationdetails.Status{}, nil
	}
	if err != nil {
		return registrationdetails.Record{}, registrationdetails.Status{}, err
	}
	raw, err := protector.Open(owner, row.EncryptedRecord)
	if err != nil {
		return registrationdetails.Record{}, registrationdetails.Status{}, err
	}
	var record registrationdetails.Record
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&record); err != nil {
		return registrationdetails.Record{}, registrationdetails.Status{}, registrationdetails.ErrProtection
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return registrationdetails.Record{}, registrationdetails.Status{}, registrationdetails.ErrProtection
	}
	status := record.Status(row.Version)
	if status.LegalNamePresent != row.LegalNamePresent || status.PassportNumberPresent != row.PassportNumberPresent {
		return registrationdetails.Record{}, registrationdetails.Status{}, registrationdetails.ErrProtection
	}
	return record, status, nil
}

func saveRegistrationDetails(
	ctx context.Context,
	tx pgx.Tx,
	owner uuid.UUID,
	record registrationdetails.Record,
	status registrationdetails.Status,
	protector *registrationdetails.Protector,
) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	encrypted, err := protector.Seal(owner, string(raw))
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_registration_details(user_id,version,encrypted_recor"+
			"d,legal_name_present,passport_number_present) VALUES($1,$2,$3,$4,$5) ON "+
			"CONFLICT(user_id) DO UPDATE SET version=EXCLUDED.version,encrypted_recor"+
			"d=EXCLUDED.encrypted_record,legal_name_present=EXCLUDED.legal_name_prese"+
			"nt,passport_number_present=EXCLUDED.passport_number_present",
		owner,
		status.Version,
		encrypted,
		status.LegalNamePresent,
		status.PassportNumberPresent,
	)
	return err
}

func replayRegistrationDetails(
	ctx context.Context,
	tx pgx.Tx,
	owner, operation uuid.UUID,
	digest []byte,
) (registrationdetails.Status, error) {
	var stored, raw []byte
	err := tx.QueryRow(ctx, "SELECT request_hash,result FROM shuttle_registration_details_operations "+
		"WHERE user_id=$1 AND operation_id=$2", owner, operation).
		Scan(&stored, &raw)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	if !bytes.Equal(stored, digest) {
		return registrationdetails.Status{}, ErrConflict
	}
	var result registrationdetails.Status
	err = json.Unmarshal(raw, &result)
	return result, err
}

// RegistrationDetailsOperation returns an owner's historical redacted receipt.
func (s *Store) RegistrationDetailsOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (registrationdetails.Status, error) {
	if operation == uuid.Nil {
		return registrationdetails.Status{}, registrationdetails.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return registrationdetails.Status{}, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT result FROM shuttle_registration_details_operations WHERE user_id"+
		"=$1 AND operation_id=$2", scope.UserID, operation).
		Scan(&raw); err != nil {
		return registrationdetails.Status{}, err
	}
	var result registrationdetails.Status
	err = json.Unmarshal(raw, &result)
	return result, err
}
