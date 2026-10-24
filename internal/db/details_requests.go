package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

const detailsRequestLifetime = 2 * time.Hour
const detailsRequestCompleted = "completed"

type DetailsRequest struct {
	ID                      uuid.UUID                        `json:"id"`
	EventID                 uuid.UUID                        `json:"event_id"`
	EventVersion            int64                            `json:"event_version"`
	EventTimezone           string                           `json:"event_timezone"`
	Requirements            registrationdetails.Requirements `json:"requirements"`
	CreatedAt               time.Time                        `json:"created_at"`
	ExpiresAt               time.Time                        `json:"expires_at"`
	CompletedAt             *time.Time                       `json:"completed_at,omitempty"`
	CompletedDetailsVersion *int64                           `json:"completed_details_version,omitempty"`
	SupersededAt            *time.Time                       `json:"superseded_at,omitempty"`
}

type DetailsRequestStatus struct {
	Request        DetailsRequest              `json:"request"`
	State          string                      `json:"state"`
	DetailsVersion int64                       `json:"details_version"`
	MissingFields  []registrationdetails.Field `json:"missing_fields"`
	ObservedAt     time.Time                   `json:"observed_at"`
}

type DetailsRequestStart struct {
	EventID              uuid.UUID `json:"event_id"`
	ExpectedEventVersion int64     `json:"expected_event_version"`
}

// BeginDetailsRequest reuses the original deadline, never a fresh window on retry.
// Data completion does not register the owner or authorize a continuation.
func (s *Store) BeginDetailsRequest(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args DetailsRequestStart,
) (DetailsRequestStatus, error) {
	if operation == uuid.Nil || args.EventID == uuid.Nil || args.ExpectedEventVersion < 1 {
		return DetailsRequestStatus{}, registrationdetails.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	digest := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if _, err = q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: args.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return DetailsRequestStatus{}, err
	}
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return DetailsRequestStatus{}, err
	}
	// Actor SHARE locks may precede event writes in administrative transactions.
	// Keep them compatible; this advisory lock serializes begins across events.
	if err = q.LockEventOperation(ctx, "details-requests:"+scope.UserID.String()); err != nil {
		return DetailsRequestStatus{}, err
	}
	prior, err := replayDetailsRequest(ctx, tx, scope.UserID, operation, digest[:])
	if !errors.Is(err, pgx.ErrNoRows) {
		return prior, err
	}
	event, err := registrationEventForChange(
		ctx,
		q,
		EventScope{BotID: scope.BotID, Organization: scope.Organization},
		args.EventID,
	)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	if event.Version != args.ExpectedEventVersion {
		return DetailsRequestStatus{}, ErrConflict
	}
	status, err := ensureDetailsRequest(ctx, tx, q, scope.UserID, event)
	if err != nil {
		return status, err
	}
	if err = insertDetailsRequestReceipt(ctx, tx, scope.UserID, operation, digest[:], status); err != nil {
		return DetailsRequestStatus{}, err
	}

	return status, tx.Commit(ctx)
}

func insertDetailsRequestReceipt(
	ctx context.Context,
	tx pgx.Tx,
	owner, operation uuid.UUID,
	digest []byte,
	status DetailsRequestStatus,
) error {
	encoded, err := json.Marshal(status)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_registration_details_request_operations(user_id,oper" +
"ation_id,request_hash,result) VALUES($1,$2,$3,$4)",
		owner,
		operation,
		digest,
		encoded,
	)
	return err
}

func nativeDetailsFailure(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	scope EventScope,
	change RegistrationChange,
	selection *RegistrationEntry,
	cause error,
) error {
	if !errors.Is(cause, registrationdetails.ErrRequired) || selection == nil ||
		selection.DetailsOperation == uuid.Nil {
		return cause
	}
	return nativeDetailsOutcome(ctx, tx, q, scope, change, selection, true)
}

// A native task that encountered missing fields must never turn into a join on retry.
// The expectation receipt commits in the same transaction as the missing decision.
func nativeDetailsOutcome(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	scope EventScope,
	change RegistrationChange,
	selection *RegistrationEntry,
	create bool,
) error {
	if selection == nil || selection.DetailsOperation == uuid.Nil {
		return nil
	}
	args := DetailsRequestStart{EventID: change.EventID, ExpectedEventVersion: selection.EventVersion}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	_, err = replayDetailsRequest(ctx, tx, change.UserID, selection.DetailsOperation, digest[:])
	if err == nil {
		return registrationdetails.ErrRequired
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if !create {
		return nil
	}
	event, err := registrationEventForChange(ctx, q, scope, change.EventID)
	if err != nil {
		return err
	}
	if err = q.LockEventOperation(ctx, "details-requests:"+change.UserID.String()); err != nil {
		return err
	}
	status, err := ensureDetailsRequest(ctx, tx, q, change.UserID, event)
	if err != nil {
		return err
	}
	if err = insertDetailsRequestReceipt(
		ctx,
		tx,
		change.UserID,
		selection.DetailsOperation,
		digest[:],
		status,
	); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return registrationdetails.ErrRequired
}

func ensureDetailsRequest(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	owner uuid.UUID,
	event events.Event,
) (DetailsRequestStatus, error) {
	status, err := registrationDetailsStatus(ctx, q, owner)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	required := registrationdetails.Requirements{}
	if event.Document.Registration.DetailsRequirements != nil {
		required = event.Document.Registration.DetailsRequirements.Normalize()
	}
	previous, err := q.LatestRegistrationDetailsRequest(
		ctx,
		sqlc.LatestRegistrationDetailsRequestParams{UserID: owner, EventID: event.ID},
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return DetailsRequestStatus{}, err
	}
	missing := required.Missing(status)
	if err == nil && previous.ExpiresAt.Time.After(now.Time) && previous.EventVersion == event.Version &&
		previous.LegalNameRequired == required.LegalName && previous.PassportNumberRequired == required.PassportNumber &&
		(!previous.CompletedAt.Valid || len(missing) == 0) {
		return detailsRequestStatus(previous, status, now.Time), nil
	}
	_, err = tx.Exec(
		ctx,
		"UPDATE shuttle_registration_details_requests SET superseded_at=$3 WHERE " +
"user_id=$1 AND event_id=$2 AND completed_at IS NULL AND superseded_at IS" +
" NULL",
		owner,
		event.ID,
		now.Time,
	)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	expires := now.Time.Add(detailsRequestLifetime)
	if event.Document.Registration.ClosesAt.Before(expires) {
		expires = event.Document.Registration.ClosesAt
	}
	var completed pgtype.Timestamptz
	var completedVersion pgtype.Int8
	if len(missing) == 0 {
		completed = now
		completedVersion = pgtype.Int8{Int64: status.Version, Valid: true}
	}
	id := uuid.New()
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_registration_details_requests(id,user_id,event_id,ev" +
"ent_version,event_timezone,legal_name_required,passport_number_required," +
"created_at,expires_at,completed_at,completed_details_version) VALUES($1," +
"$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",
		id,
		owner,
		event.ID,
		event.Version,
		event.Document.Timezone,
		required.LegalName,
		required.PassportNumber,
		now.Time,
		expires,
		completed,
		completedVersion,
	)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	row, err := q.GetRegistrationDetailsRequest(ctx, sqlc.GetRegistrationDetailsRequestParams{ID: id, UserID: owner})
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	return detailsRequestStatus(row, status, now.Time), nil
}

func detailsRequestStatus(
	row sqlc.ShuttleRegistrationDetailsRequest,
	status registrationdetails.Status,
	observed time.Time,
) DetailsRequestStatus {
	request := DetailsRequest{
		ID:            row.ID,
		EventID:       row.EventID,
		EventVersion:  row.EventVersion,
		EventTimezone: row.EventTimezone,
		Requirements: registrationdetails.Requirements{
			LegalName:      row.LegalNameRequired,
			PassportNumber: row.PassportNumberRequired,
		},
		CreatedAt: row.CreatedAt.Time.UTC(),
		ExpiresAt: row.ExpiresAt.Time.UTC(),
	}
	state := "pending"
	switch {
	case row.CompletedAt.Valid:
		at := row.CompletedAt.Time.UTC()
		request.CompletedAt = &at
		version := row.CompletedDetailsVersion.Int64
		request.CompletedDetailsVersion = &version
		state = detailsRequestCompleted
	case row.SupersededAt.Valid:
		at := row.SupersededAt.Time.UTC()
		request.SupersededAt = &at
		state = "superseded"
	case !row.ExpiresAt.Time.After(observed):
		state = "expired"
	}
	return DetailsRequestStatus{
		Request:        request,
		State:          state,
		DetailsVersion: status.Version,
		MissingFields:  request.Requirements.Missing(status),
		ObservedAt:     observed.UTC(),
	}
}

// DetailsRequest returns only this owner's request and current presence metadata.
func (s *Store) DetailsRequest(ctx context.Context, scope ProfileScope, id uuid.UUID) (DetailsRequestStatus, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return DetailsRequestStatus{}, err
	}
	q := s.q.WithTx(tx)
	row, err := q.GetRegistrationDetailsRequest(
		ctx,
		sqlc.GetRegistrationDetailsRequestParams{ID: id, UserID: scope.UserID},
	)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	status, err := registrationDetailsStatus(ctx, q, scope.UserID)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	return detailsRequestStatus(row, status, now.Time), nil
}

func replayDetailsRequest(
	ctx context.Context,
	tx pgx.Tx,
	owner, operation uuid.UUID,
	digest []byte,
) (DetailsRequestStatus, error) {
	var stored, raw []byte
	if err := tx.QueryRow(ctx, "SELECT request_hash,result FROM shuttle_registration_details_request_ope" +
"rations WHERE user_id=$1 AND operation_id=$2", owner, operation).
		Scan(&stored, &raw); err != nil {
		return DetailsRequestStatus{}, err
	}
	if !bytes.Equal(stored, digest) {
		return DetailsRequestStatus{}, ErrConflict
	}
	var result DetailsRequestStatus
	err := json.Unmarshal(raw, &result)
	return result, err
}

// Only owner locking is needed: fulfillment refers to immutable requested fields,
// not current event eligibility. A continuation must check the event separately.
func resolveDetailsRequests(ctx context.Context, tx pgx.Tx, owner uuid.UUID, status registrationdetails.Status) error {
	now, err := sqlc.New(tx).RegistrationNow(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE shuttle_registration_details_requests SET completed_at=$5,complet" +
"ed_details_version=$2\nWHERE user_id=$1 AND completed_at IS NULL AND supe" +
"rseded_at IS NULL AND expires_at>$5\nAND (NOT legal_name_required OR $3) " +
"AND (NOT passport_number_required OR $4)", owner, status.Version, status.LegalNamePresent, status.PassportNumberPresent, now.Time)
	return err
}

// DetailsRequestOperation recovers the immutable begin result, not current state.
func (s *Store) DetailsRequestOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (DetailsRequestStatus, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DetailsRequestStatus{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return DetailsRequestStatus{}, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT result FROM shuttle_registration_details_request_operations WHERE" +
" user_id=$1 AND operation_id=$2", scope.UserID, operation).
		Scan(&raw); err != nil {
		return DetailsRequestStatus{}, err
	}
	var result DetailsRequestStatus
	err = json.Unmarshal(raw, &result)
	return result, err
}
