package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

var massageSpecialistID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type MassageSpecialistBinding struct {
	EventID      uuid.UUID `json:"event_id"`
	SpecialistID string    `json:"specialist_id"`
	UserID       uuid.UUID `json:"user_id"`
	Active       bool      `json:"active"`
	Version      int64     `json:"version"`
	UpdatedBy    uuid.UUID `json:"updated_by"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type MassageBindingChange struct {
	EventID                 uuid.UUID `json:"event_id"`
	SpecialistID            string    `json:"specialist_id"`
	UserID                  uuid.UUID `json:"user_id"`
	Active                  bool      `json:"active"`
	ExpectedVersion         int64     `json:"expected_version"          minimum:"0"`
	ExpectedEventVersion    int64     `json:"expected_event_version"    minimum:"1"`
	ExpectedScheduleVersion int64     `json:"expected_schedule_version" minimum:"1"`
}

type MassageBindingPreparation struct {
	ID        uuid.UUID            `json:"proposal_id"`
	Arguments MassageBindingChange `json:"arguments"`
	ExpiresAt time.Time            `json:"expires_at"`
}

func massageBindingRecord(row sqlc.ShuttleMassageSpecialistBinding) MassageSpecialistBinding {
	return MassageSpecialistBinding{
		EventID:      row.EventID,
		SpecialistID: row.SpecialistID,
		UserID:       row.UserID,
		Active:       row.Active,
		Version:      row.Version,
		UpdatedBy:    row.UpdatedBy,
		UpdatedAt:    row.UpdatedAt.Time.UTC(),
	}
}

func normalizeMassageBinding(a MassageBindingChange) ([]byte, []byte, error) {
	if a.EventID == uuid.Nil || a.UserID == uuid.Nil || !massageSpecialistID.MatchString(a.SpecialistID) ||
		a.ExpectedVersion < 0 ||
		a.ExpectedEventVersion < 1 ||
		a.ExpectedScheduleVersion < 1 {
		return nil, nil, massage.ErrInvalid
	}
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return raw, hash[:], err
}

// PrepareMassageBinding requires global administrator authorization at the API
// boundary. Public IDs and participant passes do not establish staff identity.
func (s *Store) PrepareMassageBinding(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	a MassageBindingChange,
) (MassageBindingPreparation, error) {
	return s.prepareMassageBinding(ctx, scope, operation, uuid.Nil, a, nil)
}

// AdoptMassageBinding attaches a new operation to this actor's pending proposal.
// It preserves the original parameters and expiry; completed receipts are read separately.
func (s *Store) AdoptMassageBinding(
	ctx context.Context, scope ProfileScope, operation, proposal uuid.UUID, a MassageBindingChange,
) (MassageBindingPreparation, error) {
	if proposal == uuid.Nil {
		return MassageBindingPreparation{}, massage.ErrInvalid
	}
	return s.prepareMassageBinding(ctx, scope, operation, proposal, a, nil)
}

func (s *Store) prepareMassageBinding(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	a MassageBindingChange,
	session *MiniAppMassageSession,
) (MassageBindingPreparation, error) {
	var result MassageBindingPreparation
	raw, hash, err := normalizeMassageBinding(a)
	if err != nil {
		return result, err
	}
	tx, q, event, err := s.massageBindingTx(ctx, scope, operation, a.EventID, session)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadMassageBindingOperation(
		ctx,
		sqlc.ReadMassageBindingOperationParams{ActorID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) || (proposal != uuid.Nil && prior.ProposalID != proposal) {
			return result, ErrConflict
		}
		return MassageBindingPreparation{ID: prior.ProposalID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err = validateMassageBinding(ctx, q, scope, event, a); err != nil {
		return result, err
	}
	if proposal != uuid.Nil {
		result, err = adoptMassageBinding(ctx, q, scope, operation, proposal, a, hash)
		if err != nil {
			return result, err
		}
		return result, tx.Commit(ctx)
	}
	row, err := q.InsertMassageBindingOperation(
		ctx,
		sqlc.InsertMassageBindingOperationParams{
			ActorID:      scope.UserID,
			OperationID:  operation,
			ProposalID:   uuid.New(),
			EventID:      a.EventID,
			SpecialistID: a.SpecialistID,
			RequestHash:  hash,
			Arguments:    raw,
		},
	)
	if err != nil {
		return result, err
	}
	if err = q.InsertMassageBindingOperationAlias(ctx, sqlc.InsertMassageBindingOperationAliasParams{
		ActorID: scope.UserID, OperationID: operation, CanonicalOperationID: row.OperationID,
	}); err != nil {
		return result, err
	}
	result = MassageBindingPreparation{ID: row.ProposalID, Arguments: a, ExpiresAt: row.ExpiresAt.Time.UTC()}
	return result, tx.Commit(ctx)
}

func adoptMassageBinding(ctx context.Context, q *sqlc.Queries, scope ProfileScope, operation, proposal uuid.UUID,
	a MassageBindingChange, hash []byte,
) (MassageBindingPreparation, error) {
	var result MassageBindingPreparation
	prior, err := q.ReadMassageBindingProposal(
		ctx,
		sqlc.ReadMassageBindingProposalParams{ActorID: scope.UserID, ProposalID: proposal},
	)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(prior.RequestHash, hash) || prior.EventID != a.EventID || len(prior.Result) > 0 {
		return result, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	if !prior.ExpiresAt.Time.After(now.Time) {
		return result, ErrConflict
	}
	err = q.InsertMassageBindingOperationAlias(ctx, sqlc.InsertMassageBindingOperationAliasParams{
		ActorID: scope.UserID, OperationID: operation, CanonicalOperationID: prior.OperationID,
	})
	result = MassageBindingPreparation{ID: prior.ProposalID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}
	return result, err
}

// validateMassageBinding executes under the event lock. It deliberately does not
// lock the target actor after the caller, avoiding cross-event A->B/B->A cycles.
// Revoked targets cannot use staff reads, even if revocation races a grant.
func validateMassageBinding(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	event sqlc.ShuttleEvent,
	a MassageBindingChange,
) error {
	if event.Version != a.ExpectedEventVersion {
		return ErrConflict
	}
	row, err := q.ReadMassageSpecialistBinding(
		ctx,
		sqlc.ReadMassageSpecialistBindingParams{EventID: a.EventID, SpecialistID: a.SpecialistID},
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if a.ExpectedVersion != 0 || !a.Active {
			return ErrConflict
		}
	} else if row.UserID != a.UserID || row.Version != a.ExpectedVersion {
		return ErrConflict
	}
	// Revocation remains possible after alias removal or local target revocation.
	if !a.Active {
		return nil
	}
	if !slices.Contains(event.Modules, "massage") {
		return massage.ErrUnavailable
	}
	scheduleRow, err := q.ReadMassageSchedule(ctx, a.EventID)
	if err != nil {
		return err
	}
	if scheduleRow.Version != a.ExpectedScheduleVersion {
		return ErrConflict
	}
	schedule, err := massageScheduleRecord(scheduleRow)
	if err != nil {
		return err
	}
	found := slices.ContainsFunc(
		schedule.Document.Specialists,
		func(v massage.ScheduleSpecialist) bool { return v.ID == a.SpecialistID },
	)
	if !found {
		return massage.ErrUnavailable
	}
	target, err := q.GetUser(ctx, a.UserID)
	if err != nil {
		return err
	}
	if target.BotID != scope.BotID || target.ZitadelOrg != scope.Organization || target.ZitadelSubject == "" ||
		target.Revoked {
		return pgx.ErrNoRows
	}
	return nil
}

// ChangeMassageBinding grants/revokes only the originally named identity. Same
// operation replay returns the original receipt before later version/expiry checks.
func (s *Store) ChangeMassageBinding(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	a MassageBindingChange,
	checkIdentity func(context.Context) error,
) (MassageSpecialistBinding, error) {
	return s.changeMassageBinding(ctx, scope, operation, proposal, a, checkIdentity, nil)
}

func (s *Store) changeMassageBinding(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	a MassageBindingChange,
	checkIdentity func(context.Context) error,
	session *MiniAppMassageSession,
) (MassageSpecialistBinding, error) {
	var result MassageSpecialistBinding
	_, hash, err := normalizeMassageBinding(a)
	if err != nil || proposal == uuid.Nil || checkIdentity == nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBindingTx(ctx, scope, operation, a.EventID, session)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadMassageBindingOperation(
		ctx,
		sqlc.ReadMassageBindingOperationParams{ActorID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return result, err
	}
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return result, ErrConflict
	}
	if len(prior.Result) > 0 {
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	if !prior.ExpiresAt.Time.After(now.Time) {
		return result, ErrConflict
	}
	if err = validateMassageBinding(ctx, q, scope, event, a); err != nil {
		return result, err
	}
	if err = checkIdentity(ctx); err != nil {
		return result, err
	}
	var row sqlc.ShuttleMassageSpecialistBinding
	if a.ExpectedVersion == 0 {
		row, err = q.InsertMassageSpecialistBinding(
			ctx,
			sqlc.InsertMassageSpecialistBindingParams{
				EventID:      a.EventID,
				SpecialistID: a.SpecialistID,
				UserID:       a.UserID,
				Active:       a.Active,
				UpdatedBy:    scope.UserID,
			},
		)
	} else {
		row, err = q.UpdateMassageSpecialistBinding(
			ctx,
			sqlc.UpdateMassageSpecialistBindingParams{
				EventID:         a.EventID,
				SpecialistID:    a.SpecialistID,
				UserID:          a.UserID,
				Active:          a.Active,
				ExpectedVersion: a.ExpectedVersion,
				ActorID:         scope.UserID,
			},
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrConflict
	}
	if err != nil {
		return result, err
	}
	result = massageBindingRecord(row)
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	saved, err := q.SaveMassageBindingOperation(
		ctx,
		sqlc.SaveMassageBindingOperationParams{ActorID: scope.UserID, OperationID: prior.OperationID, Result: raw},
	)
	if err != nil {
		return result, err
	}
	if saved != 1 {
		return result, ErrConflict
	}
	return result, tx.Commit(ctx)
}

// MassageBindingOperation requires current administrator authorization at the
// API boundary and only exposes this actor's immutable successful receipt.
func (s *Store) MassageBindingOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MassageSpecialistBinding, error) {
	return s.massageBindingOperation(ctx, scope, operation, nil)
}

func (s *Store) massageBindingOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	session *MiniAppMassageSession,
) (MassageSpecialistBinding, error) {
	var result MassageSpecialistBinding
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = s.lockMassageBindingActor(ctx, tx, scope, session); err != nil {
		return result, err
	}
	row, err := s.q.WithTx(tx).
		ReadMassageBindingOperation(ctx, sqlc.ReadMassageBindingOperationParams{ActorID: scope.UserID, OperationID: operation})
	if err != nil {
		return result, err
	}
	if len(row.Result) == 0 {
		return result, pgx.ErrNoRows
	}
	err = json.Unmarshal(row.Result, &result)
	return result, err
}
