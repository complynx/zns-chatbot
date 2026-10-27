package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// PrepareOwnMassageBooking freezes exact current terms without holding capacity.
func (s *Store) PrepareOwnMassageBooking(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args MassageBookingArguments,
) (MassageBookingPreparation, error) {
	var result MassageBookingPreparation
	args, err := normalizeMassageBooking(args)
	if err != nil || operation == uuid.Nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.prepareOwnMassageBooking(ctx, q, scope, event, operation, args, MassageSourceAgent)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) prepareOwnMassageBooking(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	event sqlc.ShuttleEvent,
	operation uuid.UUID,
	args MassageBookingArguments,
	source MassageBookingSource,
) (MassageBookingPreparation, bool, error) {
	var result MassageBookingPreparation
	hash, err := massageRequestHash("book", args, uuid.Nil)
	if err != nil {
		return result, false, err
	}
	row, err := q.ReadMassagePreparationByOperation(
		ctx,
		sqlc.ReadMassagePreparationByOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !massageProposalValid(row, "book", hash) {
			return result, false, ErrConflict
		}
		err = json.Unmarshal(row.Snapshot, &result.Snapshot)
		result.ID, result.Arguments, result.ExpiresAt = row.ID, args, row.ExpiresAt.Time.UTC()
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	snapshot, err := s.currentMassageBookingSnapshot(ctx, q, scope, event, args)
	if err != nil {
		return result, false, err
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return result, false, err
	}
	rawSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return result, false, err
	}
	row, err = q.InsertMassagePreparation(
		ctx,
		sqlc.InsertMassagePreparationParams{
			ID:          uuid.New(),
			EventID:     args.EventID,
			UserID:      scope.UserID,
			OperationID: operation,
			Kind:        "book",
			Source:      string(source),
			RequestHash: hash,
			Arguments:   rawArgs,
			Snapshot:    rawSnapshot,
		},
	)
	if err != nil {
		return result, false, err
	}
	result = MassageBookingPreparation{
		ID:        row.ID,
		Arguments: args,
		Snapshot:  snapshot,
		ExpiresAt: row.ExpiresAt.Time.UTC(),
	}
	return result, false, nil
}

// BookOwnMassage shares the prepared proposal across interfaces. Its original
// result survives cancellation, later configuration and a lost commit response.
func (s *Store) BookOwnMassage(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	args MassageBookingArguments,
	source MassageBookingSource,
) (MassageBooking, error) {
	var result MassageBooking
	args, err := normalizeMassageBooking(args)
	if err != nil || operation == uuid.Nil || proposal == uuid.Nil || !massageSourceValid(source) {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.bookOwnMassage(ctx, q, scope, event, operation, proposal, args, source)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) bookOwnMassage(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	event sqlc.ShuttleEvent,
	operation uuid.UUID,
	proposal uuid.UUID,
	args MassageBookingArguments,
	source MassageBookingSource,
) (MassageBooking, bool, error) {
	var result MassageBooking
	hash, err := massageRequestHash("book", args, proposal)
	if err != nil {
		return result, false, err
	}
	if prior, replayed, priorErr := massagePriorResult(
		ctx,
		q,
		scope,
		args.EventID,
		operation,
		"book",
		hash,
	); priorErr != nil ||
		replayed {
		return prior, replayed, priorErr
	}
	prepared, err := readMassageApplyProposal(ctx, q, scope, args.EventID, proposal, "book", args)
	if err != nil {
		return result, false, err
	}
	if prepared.ConsumedAt.Valid {
		result, err = massageConsumedResult(ctx, q, scope, operation, "book", hash, prepared, source)
		if err != nil {
			return result, false, err
		}
		return result, false, nil
	}
	snapshot, err := s.currentMassageBookingSnapshot(ctx, q, scope, event, args)
	if err != nil {
		return result, false, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return result, false, err
	}
	if err = matchMassageSnapshot(prepared.Snapshot, raw); err != nil {
		return result, false, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, false, err
	}
	if !prepared.ExpiresAt.Time.After(now.Time) ||
		snapshot.Terms.StartsAt.Before(now.Time.Add(massage.ClientBookingLead)) {
		return result, false, ErrConflict
	}
	result, err = insertMassageBooking(ctx, q, scope, args.EventID, proposal, snapshot, raw, source)
	if err != nil {
		return result, false, err
	}
	if err = consumeMassageProposal(ctx, q, proposal, result); err != nil {
		return result, false, err
	}
	if err = insertMassageReceipt(ctx, q, scope, operation, "book", hash, proposal, result, source); err != nil {
		return result, false, err
	}
	return result, false, nil
}

func consumeMassageProposal(ctx context.Context, q *sqlc.Queries, proposal uuid.UUID, result MassageBooking) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	changed, err := q.ConsumeMassagePreparation(
		ctx,
		sqlc.ConsumeMassagePreparationParams{
			ID:             proposal,
			BookingID:      pgtype.UUID{Bytes: result.ID, Valid: true},
			ConsumedResult: raw,
		},
	)
	if err == nil && changed != 1 {
		return ErrConflict
	}
	return err
}

func matchMassageSnapshot(prepared, current []byte) error {
	var approved MassageBookingSnapshot
	if err := json.Unmarshal(prepared, &approved); err != nil {
		return err
	}
	raw, err := json.Marshal(approved)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, raw) {
		return ErrConflict
	}
	return nil
}

func insertMassageBooking(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	eventID, proposal uuid.UUID, snapshot MassageBookingSnapshot, raw []byte, source MassageBookingSource,
) (MassageBooking, error) {
	terms := snapshot.Terms
	if terms.CurrencyScale < 0 || terms.CurrencyScale > massage.MaxCurrencyScale {
		return MassageBooking{}, massage.ErrInvalid
	}
	row, err := q.InsertMassageBooking(ctx, sqlc.InsertMassageBookingParams{
		ID: uuid.New(), EventID: eventID, UserID: scope.UserID, PreparationID: proposal,
		WindowID: terms.WindowID, SpecialistID: terms.SpecialistID, ServiceID: terms.ServiceID,
		StartsAt: massageTimestamp(terms.StartsAt), TreatmentEndsAt: massageTimestamp(terms.TreatmentEndsAt),
		OccupancyEndsAt: massageTimestamp(terms.OccupancyEndsAt), RequiresTable: terms.RequiresTable,
		Currency: terms.Currency, CurrencyScale: int32(terms.CurrencyScale), PriceMinor: terms.PriceMinor,
		Snapshot: raw, Source: string(source),
	})
	if err != nil {
		return MassageBooking{}, err
	}
	if err = insertMassageReminders(ctx, q, row.ID); err != nil {
		return MassageBooking{}, err
	}
	if snapshot.Purpose != massageInstantPurpose {
		if err = q.InsertMassageSpecialistNotification(
			ctx,
			sqlc.InsertMassageSpecialistNotificationParams{
				ID:        uuid.New(),
				BookingID: row.ID,
				Kind:      massageBookingStateBooked,
			},
		); err != nil {
			return MassageBooking{}, err
		}
	}
	return massageBookingRecord(row)
}
