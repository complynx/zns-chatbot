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
	"github.com/complynx/zns-chatbot/internal/massage"
)

// massageBookingTx serializes operation -> event -> current owner. No network IO
// runs under these locks. Historical receipt/cancellation access needs no new pass.
func (s *Store) massageBookingTx(
	ctx context.Context,
	scope ProfileScope,
	eventID, operation uuid.UUID,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, sqlc.ShuttleEvent{}, err
	}
	q := s.q.WithTx(tx)
	if operation != uuid.Nil {
		err = q.LockEventOperation(ctx, fmt.Sprintf("massage-booking:%s:%s", scope.UserID, operation))
	}
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
		return nil, nil, event, err
	}
	return tx, q, event, nil
}

func massageRequestHash(kind string, args any, proposal uuid.UUID) ([]byte, error) {
	raw, err := json.Marshal(struct {
		Kind      string    `json:"kind"`
		Arguments any       `json:"arguments"`
		Proposal  uuid.UUID `json:"proposal_id"`
	}{kind, args, proposal})
	hash := sha256.Sum256(raw)
	return hash[:], err
}

func massageBookingRecord(row sqlc.ShuttleMassageBooking) (MassageBooking, error) {
	result := MassageBooking{
		ID:       row.ID,
		EventID:  row.EventID,
		Version:  row.Version,
		State:    row.State,
		Source:   MassageBookingSource(row.Source),
		BookedAt: row.BookedAt.Time.UTC(),
	}
	if row.CancelledAt.Valid {
		at := row.CancelledAt.Time.UTC()
		result.CancelledAt = &at
	}
	err := json.Unmarshal(row.Snapshot, &result.Snapshot)
	return result, err
}

func massagePriorResult(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	eventID, operation uuid.UUID,
	kind string,
	hash []byte,
) (MassageBooking, bool, error) {
	prior, err := q.ReadMassageOperation(
		ctx,
		sqlc.ReadMassageOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MassageBooking{}, false, nil
	}
	if err != nil {
		return MassageBooking{}, false, err
	}
	if prior.EventID != eventID || prior.Kind != kind || !bytes.Equal(prior.RequestHash, hash) {
		return MassageBooking{}, false, ErrConflict
	}
	var result MassageBooking
	err = json.Unmarshal(prior.Result, &result)
	return result, true, err
}

func insertMassageReceipt(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	operation uuid.UUID,
	kind string,
	hash []byte,
	proposal uuid.UUID,
	result MassageBooking,
	source MassageBookingSource,
) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return q.InsertMassageOperation(
		ctx,
		sqlc.InsertMassageOperationParams{
			UserID:        scope.UserID,
			OperationID:   operation,
			EventID:       result.EventID,
			Kind:          kind,
			RequestHash:   hash,
			PreparationID: proposal,
			BookingID:     result.ID,
			Result:        raw,
			Source:        string(source),
		},
	)
}

func massageProposalValid(row sqlc.ShuttleMassagePreparation, kind string, hash []byte) bool {
	return row.Kind == kind && bytes.Equal(row.RequestHash, hash)
}

func (s *Store) currentMassageBookingSnapshot(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	event sqlc.ShuttleEvent,
	args MassageBookingArguments,
) (MassageBookingSnapshot, error) {
	var snapshot MassageBookingSnapshot
	schedule, maximum, err := massageBookingConfiguration(
		ctx,
		q,
		scope,
		event,
		args.ExpectedEventVersion,
		args.ExpectedScheduleVersion,
	)
	if err != nil {
		return snapshot, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return snapshot, err
	}
	terms, err := massage.PublicBookingTerms(schedule.Document, args.Selection, now.Time, event.Timezone)
	if err != nil {
		return snapshot, err
	}
	count, err := q.CountOwnPartyMassages(
		ctx,
		sqlc.CountOwnPartyMassagesParams{EventID: event.ID, UserID: scope.UserID, WindowID: terms.WindowID},
	)
	if err != nil {
		return snapshot, err
	}
	if count >= int64(maximum) {
		return snapshot, massage.ErrUnavailable
	}
	rows, err := q.ReadMassageBookingOccupations(
		ctx,
		sqlc.ReadMassageBookingOccupationsParams{
			EventID: event.ID,
			FromAt:  massageTimestamp(terms.StartsAt),
			UntilAt: massageTimestamp(terms.OccupancyEndsAt),
		},
	)
	if err != nil {
		return snapshot, err
	}
	occupied := massageOccupations(rows, scope.UserID, terms.SpecialistID)
	if !massage.BookingSlotAvailable(terms, occupied) {
		return snapshot, massage.ErrUnavailable
	}
	return MassageBookingSnapshot{
		EventName:            event.Name,
		EventVersion:         event.Version,
		ScheduleVersion:      schedule.Version,
		MaxBookingsPerWindow: maximum,
		Terms:                terms,
	}, nil
}

func massageTimestamp(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at.UTC(), Valid: true}
}

func massageOccupations(
	rows []sqlc.ReadMassageBookingOccupationsRow,
	user uuid.UUID,
	specialist string,
) []massage.BookingOccupation {
	result := make([]massage.BookingOccupation, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			massage.BookingOccupation{
				StartsAt:       row.StartsAt.Time,
				EndsAt:         row.OccupancyEndsAt.Time,
				SameOwner:      row.UserID == user,
				SameSpecialist: row.SpecialistID == specialist,
				RequiresTable:  row.RequiresTable,
			},
		)
	}
	return result
}

// readMassageApplyProposal binds the proposal to the current owner and exact business arguments.
func readMassageApplyProposal(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	eventID, proposal uuid.UUID, kind string, args any,
) (sqlc.ShuttleMassagePreparation, error) {
	prepared, err := q.ReadMassagePreparation(ctx, sqlc.ReadMassagePreparationParams{
		ID: proposal, EventID: eventID, UserID: scope.UserID,
	})
	if err != nil {
		return prepared, err
	}
	business, err := massageRequestHash(kind, args, uuid.Nil)
	if err != nil {
		return prepared, err
	}
	if !massageProposalValid(prepared, kind, business) {
		return prepared, ErrConflict
	}
	return prepared, nil
}

// massageConsumedResult records a new interface operation against the canonical result.
// It does not create another booking or reapply a cancellation.
func massageConsumedResult(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	operation uuid.UUID, kind string, hash []byte, prepared sqlc.ShuttleMassagePreparation, source MassageBookingSource,
) (MassageBooking, error) {
	var result MassageBooking
	if err := json.Unmarshal(prepared.ConsumedResult, &result); err != nil {
		return result, err
	}
	return result, insertMassageReceipt(ctx, q, scope, operation, kind, hash, prepared.ID, result, source)
}

// massageBookingConfiguration shares current entitlement, versions and policy
// across preparation, application and availability. The caller authorizes and
// locks the current owner before entry and owns the transaction.
func massageBookingConfiguration(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, expectedEvent, expectedSchedule int64,
) (MassageSchedule, int, error) {
	var scheduleResult MassageSchedule
	allowed, err := q.UserModuleAvailable(
		ctx,
		sqlc.UserModuleAvailableParams{
			ID:             event.ID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ID_2:           scope.UserID,
			Column5:        "massage",
		},
	)
	if err != nil {
		return scheduleResult, 0, err
	}
	if !allowed {
		return scheduleResult, 0, pgx.ErrNoRows
	}
	return massageScheduleConfiguration(ctx, q, event, expectedEvent, expectedSchedule)
}

// massageScheduleConfiguration checks configuration only. The caller must first
// authorize either ordinary participation or a private specialist binding.
func massageScheduleConfiguration(ctx context.Context, q *sqlc.Queries,
	event sqlc.ShuttleEvent, expectedEvent, expectedSchedule int64,
) (MassageSchedule, int, error) {
	var scheduleResult MassageSchedule
	scheduleRow, err := q.ReadMassageSchedule(ctx, event.ID)
	if err != nil {
		return scheduleResult, 0, err
	}
	schedule, err := massageScheduleRecord(scheduleRow)
	if err != nil {
		return scheduleResult, 0, err
	}
	if event.Version != expectedEvent || schedule.Version != expectedSchedule ||
		!massage.FitsEvent(schedule.Document, event.StartsAt.Time, event.EndsAt.Time) {
		return scheduleResult, 0, ErrConflict
	}
	settings, err := q.ReadSettingsForMassage(ctx)
	if err != nil {
		return scheduleResult, 0, err
	}
	var values Settings
	if err = json.Unmarshal(settings.Document, &values); err != nil {
		return scheduleResult, 0, err
	}
	maximum := values.MassageMaxBookingsPerWindow
	if maximum == 0 {
		maximum = defaultMassageMaxBookingsPerWindow
	}
	if maximum < 1 || maximum > 100 {
		return scheduleResult, 0, massage.ErrInvalid
	}
	return schedule, maximum, nil
}
