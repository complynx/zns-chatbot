package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// PrepareOwnMassageCancellation uses the current owned record, not mutable
// schedule versions or a new pass. Legacy cancellation has no time/fee cutoff.
func (s *Store) PrepareOwnMassageCancellation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args MassageCancellationArguments,
) (MassageCancellationPreparation, error) {
	var result MassageCancellationPreparation
	if operation == uuid.Nil || !validMassageCancellation(args) {
		return result, massage.ErrInvalid
	}
	tx, q, _, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.prepareOwnMassageCancellation(ctx, q, scope, operation, args, MassageSourceAgent)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) prepareOwnMassageCancellation(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	operation uuid.UUID,
	args MassageCancellationArguments,
	source MassageBookingSource,
) (MassageCancellationPreparation, bool, error) {
	var result MassageCancellationPreparation
	hash, err := massageRequestHash("cancel", args, uuid.Nil)
	if err != nil {
		return result, false, err
	}
	prepared, err := q.ReadMassagePreparationByOperation(
		ctx,
		sqlc.ReadMassagePreparationByOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !massageProposalValid(prepared, "cancel", hash) {
			return result, false, ErrConflict
		}
		err = json.Unmarshal(prepared.Snapshot, &result.Booking)
		result.ID, result.Arguments, result.ExpiresAt = prepared.ID, args, prepared.ExpiresAt.Time.UTC()
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	row, err := q.ReadMassageBooking(
		ctx,
		sqlc.ReadMassageBookingParams{ID: args.BookingID, EventID: args.EventID, UserID: scope.UserID},
	)
	if err != nil {
		return result, false, err
	}
	if row.State != "booked" || row.Version != args.ExpectedVersion {
		return result, false, ErrConflict
	}
	booking, err := massageBookingRecord(row)
	if err != nil {
		return result, false, err
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return result, false, err
	}
	rawBooking, err := json.Marshal(booking)
	if err != nil {
		return result, false, err
	}
	prepared, err = q.InsertMassagePreparation(
		ctx,
		sqlc.InsertMassagePreparationParams{
			ID:          uuid.New(),
			EventID:     args.EventID,
			UserID:      scope.UserID,
			OperationID: operation,
			Kind:        "cancel",
			Source:      string(source),
			RequestHash: hash,
			Arguments:   rawArgs,
			Snapshot:    rawBooking,
			BookingID:   pgtype.UUID{Bytes: args.BookingID, Valid: true},
		},
	)
	if err != nil {
		return result, false, err
	}
	result = MassageCancellationPreparation{
		ID:        prepared.ID,
		Arguments: args,
		Booking:   booking,
		ExpiresAt: prepared.ExpiresAt.Time.UTC(),
	}
	return result, false, nil
}

// CancelOwnMassage retains the booking and original book receipt while releasing
// its occupied resources. It never creates a refund, fee or payment confirmation.
func (s *Store) CancelOwnMassage(
	ctx context.Context,
	scope ProfileScope,
	operation, proposal uuid.UUID,
	args MassageCancellationArguments,
) (MassageBooking, error) {
	var result MassageBooking
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageCancellation(args) {
		return result, massage.ErrInvalid
	}
	tx, q, _, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.cancelOwnMassage(ctx, q, scope, operation, proposal, args, MassageSourceAgent)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) cancelOwnMassage(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	operation uuid.UUID,
	proposal uuid.UUID,
	args MassageCancellationArguments,
	source MassageBookingSource,
) (MassageBooking, bool, error) {
	var result MassageBooking
	hash, err := massageRequestHash("cancel", args, proposal)
	if err != nil {
		return result, false, err
	}
	if prior, replayed, priorErr := massagePriorResult(
		ctx,
		q,
		scope,
		args.EventID,
		operation,
		"cancel",
		hash,
	); priorErr != nil ||
		replayed {
		return prior, replayed, priorErr
	}
	prepared, err := readMassageApplyProposal(ctx, q, scope, args.EventID, proposal, "cancel", args)
	if err != nil {
		return result, false, err
	}
	if prepared.ConsumedAt.Valid {
		result, err = massageConsumedResult(ctx, q, scope, operation, "cancel", hash, prepared, source)
		if err != nil {
			return result, false, err
		}
		return result, false, nil
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, false, err
	}
	if !prepared.ExpiresAt.Time.After(now.Time) {
		return result, false, ErrConflict
	}
	row, err := q.CancelMassageBooking(
		ctx,
		sqlc.CancelMassageBookingParams{
			ID:      args.BookingID,
			EventID: args.EventID,
			UserID:  scope.UserID,
			Version: args.ExpectedVersion,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, false, ErrConflict
	}
	if err != nil {
		return result, false, err
	}
	if err = blockBookingMassageReminders(ctx, q, row.ID); err != nil {
		return result, false, err
	}
	if err = q.BlockBookedMassageSpecialistNotification(ctx, row.ID); err != nil {
		return result, false, err
	}
	if err = q.InsertMassageSpecialistNotification(
		ctx,
		sqlc.InsertMassageSpecialistNotificationParams{ID: uuid.New(), BookingID: row.ID, Kind: "cancelled"},
	); err != nil {
		return result, false, err
	}
	result, err = massageBookingRecord(row)
	if err != nil {
		return result, false, err
	}
	if err = consumeMassageProposal(ctx, q, proposal, result); err != nil {
		return result, false, err
	}
	if err = insertMassageReceipt(ctx, q, scope, operation, "cancel", hash, proposal, result, source); err != nil {
		return result, false, err
	}
	return result, false, nil
}
