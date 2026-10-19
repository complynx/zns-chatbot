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

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

type MassageSchedule struct {
	EventID  uuid.UUID                `json:"event_id"`
	Version  int64                    `json:"version"`
	Document massage.ScheduleDocument `json:"document"`
}

type MassageScheduleChange struct {
	EventID              uuid.UUID                `json:"event_id"`
	ExpectedEventVersion int64                    `json:"expected_event_version" minimum:"1"`
	ExpectedVersion      int64                    `json:"expected_version"       minimum:"0"`
	Document             massage.ScheduleDocument `json:"document"`
}

type MassageSchedulePreparation struct {
	ID        uuid.UUID             `json:"proposal_id"`
	Arguments MassageScheduleChange `json:"arguments"`
	ExpiresAt time.Time             `json:"expires_at"`
}

type MassageScheduleView struct {
	MassageSchedule

	EventVersion         int64     `json:"event_version"`
	EventName            string    `json:"event_name"`
	Timezone             string    `json:"timezone"`
	ObservedAt           time.Time `json:"observed_at"`
	ScheduleOnly         bool      `json:"schedule_only"`
	ReservationSupported bool      `json:"reservation_supported"`
}

func massageScheduleRecord(row sqlc.ShuttleMassageSchedule) (MassageSchedule, error) {
	result := MassageSchedule{EventID: row.EventID, Version: row.Version}
	// PostgreSQL jsonb adds whitespace; the model bounds compact document bytes.
	var compact bytes.Buffer
	if err := json.Compact(&compact, row.Document); err != nil {
		return result, err
	}
	if err := json.Unmarshal(compact.Bytes(), &result.Document); err != nil {
		return result, err
	}
	normalized, err := massage.Normalize(result.Document)
	result.Document = normalized
	return result, err
}

// MassageScheduleConfiguration is an administrative configuration read. The API
// requires the global administrator role; ordinary owners use OwnMassageSchedule.
func (s *Store) MassageScheduleConfiguration(
	ctx context.Context,
	scope ProfileScope,
	eventID uuid.UUID,
) (MassageSchedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MassageSchedule{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return MassageSchedule{}, err
	}
	q := s.q.WithTx(tx)
	if _, err = q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return MassageSchedule{}, err
	}
	row, err := q.ReadMassageSchedule(ctx, eventID)
	if err != nil {
		return MassageSchedule{}, err
	}
	return massageScheduleRecord(row)
}

// OwnMassageSchedule requires the caller's assigned pass, including unpaid/overdue.
// Current binding, module entitlement and schedule share one transaction connection.
func (s *Store) OwnMassageSchedule(
	ctx context.Context, scope ProfileScope, eventID uuid.UUID,
) (MassageScheduleView, error) {
	result := MassageScheduleView{ScheduleOnly: true}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return result, err
	}
	q := s.q.WithTx(tx)
	return readOwnMassageSchedule(ctx, q, scope, eventID)
}

func readOwnMassageSchedule(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	eventID uuid.UUID,
) (MassageScheduleView, error) {
	result := MassageScheduleView{ScheduleOnly: true}
	allowed, err := q.UserModuleAvailable(ctx, sqlc.UserModuleAvailableParams{
		ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization, ID_2: scope.UserID, Column5: "massage"})
	if err != nil {
		return result, err
	}
	if !allowed {
		return result, pgx.ErrNoRows
	}
	event, err := q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return result, err
	}
	return readMassageScheduleView(ctx, q, event)
}

func readMassageScheduleView(
	ctx context.Context,
	q *sqlc.Queries,
	event sqlc.ShuttleEvent,
) (MassageScheduleView, error) {
	result := MassageScheduleView{ScheduleOnly: true}
	row, err := q.ReadMassageSchedule(ctx, event.ID)
	if err != nil {
		return result, err
	}
	schedule, err := massageScheduleRecord(row)
	if err != nil {
		return result, err
	}
	if !massage.FitsEvent(schedule.Document, event.StartsAt.Time, event.EndsAt.Time) {
		return result, ErrConflict
	}
	zone, err := time.LoadLocation(event.Timezone)
	if err != nil {
		return result, err
	}
	for index := range schedule.Document.Windows {
		window := &schedule.Document.Windows[index]
		window.StartsAt, window.EndsAt = window.StartsAt.In(zone), window.EndsAt.In(zone)
	}
	for index := range schedule.Document.Specialists {
		for spanIndex := range schedule.Document.Specialists[index].WorkSpans {
			span := &schedule.Document.Specialists[index].WorkSpans[spanIndex]
			span.StartsAt, span.EndsAt = span.StartsAt.In(zone), span.EndsAt.In(zone)
		}
	}
	if _, err = json.Marshal(schedule.Document); err != nil {
		return result, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	result.MassageSchedule, result.EventVersion, result.EventName = schedule, event.Version, event.Name
	result.Timezone, result.ObservedAt = event.Timezone, now.Time.UTC()
	return result, nil
}

func normalizeMassageChange(a MassageScheduleChange) (MassageScheduleChange, []byte, error) {
	if a.EventID == uuid.Nil || a.ExpectedEventVersion < 1 || a.ExpectedVersion < 0 {
		return a, nil, massage.ErrInvalid
	}
	document, err := massage.Normalize(a.Document)
	if err != nil {
		return a, nil, err
	}
	a.Document = document
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return a, hash[:], err
}

// Lock order is event then current actor, matching other event mutations.
func (s *Store) massageScheduleTx(
	ctx context.Context,
	scope ProfileScope,
	operation, eventID uuid.UUID,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, error) {
	return s.massageEventWriteTx(ctx, scope, operation, eventID, "massage-schedule")
}

func (s *Store) massageEventWriteTx(
	ctx context.Context,
	scope ProfileScope,
	operation, eventID uuid.UUID,
	kind string,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, error) {
	if operation == uuid.Nil {
		return nil, nil, sqlc.ShuttleEvent{}, massage.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, sqlc.ShuttleEvent{}, err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(ctx, fmt.Sprintf("%s:%s:%s", kind, scope.UserID, operation))
	var event sqlc.ShuttleEvent
	if err == nil {
		event, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
	if err == nil {
		err = lockProfileOwner(ctx, tx, scope, false)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, sqlc.ShuttleEvent{}, err
	}
	return tx, q, event, nil
}

func massageScheduleVersion(ctx context.Context, q *sqlc.Queries, eventID uuid.UUID, expected int64) error {
	row, err := q.ReadMassageSchedule(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) && expected == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Version != expected {
		return ErrConflict
	}
	return nil
}

// PrepareMassageSchedule records a five-minute exact proposal without changing the schedule.
func (s *Store) PrepareMassageSchedule(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	a MassageScheduleChange,
) (MassageSchedulePreparation, error) {
	var result MassageSchedulePreparation
	a, hash, err := normalizeMassageChange(a)
	if err != nil {
		return result, err
	}
	tx, q, event, err := s.massageScheduleTx(ctx, scope, operation, a.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadMassageSchedulePreparation(
		ctx,
		sqlc.ReadMassageSchedulePreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return result, ErrConflict
		}
		return MassageSchedulePreparation{ID: prior.ID, Arguments: a, ExpiresAt: prior.ExpiresAt.Time.UTC()}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if event.Version != a.ExpectedEventVersion {
		return result, ErrConflict
	}
	if !massage.FitsEvent(a.Document, event.StartsAt.Time, event.EndsAt.Time) {
		return result, ErrConflict
	}
	if err = massageScheduleVersion(ctx, q, a.EventID, a.ExpectedVersion); err != nil {
		return result, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return result, err
	}
	row, err := q.InsertMassageSchedulePreparation(
		ctx,
		sqlc.InsertMassageSchedulePreparationParams{
			ID:          uuid.New(),
			UserID:      scope.UserID,
			OperationID: operation,
			RequestHash: hash,
			Arguments:   raw,
		},
	)
	if err != nil {
		return result, err
	}
	result = MassageSchedulePreparation{ID: row.ID, Arguments: a, ExpiresAt: row.ExpiresAt.Time.UTC()}
	return result, tx.Commit(ctx)
}

// ChangeMassageSchedule atomically stores the normalized schedule and original receipt.
// A successful same-operation replay precedes expiry/version checks.
func (s *Store) ChangeMassageSchedule(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	a MassageScheduleChange,
	checkIdentity func(context.Context) error,
) (MassageSchedule, error) {
	var result MassageSchedule
	a, hash, err := normalizeMassageChange(a)
	if err != nil || proposal == uuid.Nil || checkIdentity == nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageScheduleTx(ctx, scope, operation, a.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.ReadMassageScheduleOperation(
		ctx,
		sqlc.ReadMassageScheduleOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return result, ErrConflict
		}
		err = decodeMassageReceipt(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	prepared, err := q.ReadMassageSchedulePreparation(
		ctx,
		sqlc.ReadMassageSchedulePreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	if prepared.ID != proposal || !bytes.Equal(prepared.RequestHash, hash) ||
		!prepared.ExpiresAt.Time.After(now.Time) ||
		event.Version != a.ExpectedEventVersion {
		return result, ErrConflict
	}
	if !massage.FitsEvent(a.Document, event.StartsAt.Time, event.EndsAt.Time) {
		return result, ErrConflict
	}
	if err = massageScheduleVersion(ctx, q, a.EventID, a.ExpectedVersion); err != nil {
		return result, err
	}
	if err = checkIdentity(ctx); err != nil {
		return result, err
	}
	if err = validateMassageCommitments(ctx, q, a.EventID, a.Document); err != nil {
		return result, err
	}
	retained := make([]string, len(a.Document.Specialists))
	for i, specialist := range a.Document.Specialists {
		retained[i] = specialist.ID
	}
	if _, err = q.DeactivateRemovedMassageSpecialists(
		ctx,
		sqlc.DeactivateRemovedMassageSpecialistsParams{
			EventID:     a.EventID,
			ActorID:     scope.UserID,
			RetainedIds: retained,
		},
	); err != nil {
		return result, err
	}
	result, err = writeMassageSchedule(ctx, q, a)
	if err != nil {
		return result, err
	}
	receipt, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if err = q.InsertMassageScheduleOperation(ctx, sqlc.InsertMassageScheduleOperationParams{
		UserID: scope.UserID, OperationID: operation, RequestHash: hash, Result: receipt,
	}); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func writeMassageSchedule(ctx context.Context, q *sqlc.Queries, a MassageScheduleChange) (MassageSchedule, error) {
	var result MassageSchedule
	raw, err := json.Marshal(a.Document)
	if err != nil {
		return result, err
	}
	var row sqlc.ShuttleMassageSchedule
	if a.ExpectedVersion == 0 {
		row, err = q.InsertMassageSchedule(ctx, sqlc.InsertMassageScheduleParams{EventID: a.EventID, Document: raw})
	} else {
		row, err = q.UpdateMassageSchedule(
			ctx,
			sqlc.UpdateMassageScheduleParams{EventID: a.EventID, Document: raw, Version: a.ExpectedVersion},
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrConflict
	}
	if err != nil {
		return result, err
	}
	return massageScheduleRecord(row)
}

func (s *Store) MassageScheduleOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MassageSchedule, error) {
	var result MassageSchedule
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return result, err
	}
	row, err := s.q.WithTx(tx).
		ReadMassageScheduleOperation(ctx, sqlc.ReadMassageScheduleOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return result, err
	}
	err = decodeMassageReceipt(row.Result, &result)
	return result, err
}

func decodeMassageReceipt(raw []byte, result *MassageSchedule) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return err
	}
	return json.Unmarshal(compact.Bytes(), result)
}
