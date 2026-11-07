package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// AdoptOwnMassageBooking revalidates the exact existing proposal for a new
// interface operation. It creates no replacement, hold, receipt or renewed expiry.
func (s *Store) AdoptOwnMassageBooking(ctx context.Context, scope ProfileScope,
	operation, proposal uuid.UUID, args MassageBookingArguments,
) (MassageBookingPreparation, error) {
	var result MassageBookingPreparation
	args, err := normalizeMassageBooking(args)
	if err != nil || operation == uuid.Nil || proposal == uuid.Nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return s.adoptMassageBooking(ctx, q, scope, event, proposal, args)
}

func (s *Store) AdoptMiniAppMassageBooking(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation, proposal uuid.UUID, args MassageBookingArguments,
) (MassageBookingPreparation, error) {
	var result MassageBookingPreparation
	args, err := normalizeMassageBooking(args)
	if err != nil || operation == uuid.Nil || proposal == uuid.Nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return s.adoptMassageBooking(ctx, q, scope, event, proposal, args)
}

func (s *Store) adoptMassageBooking(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, proposal uuid.UUID, args MassageBookingArguments,
) (MassageBookingPreparation, error) {
	var result MassageBookingPreparation
	row, err := activeMassageProposal(ctx, q, scope, args.EventID, proposal, "book", args)
	if err != nil {
		return result, err
	}
	snapshot, err := s.currentMassageBookingSnapshot(ctx, q, scope, event, args)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return result, err
	}
	if err = matchMassageSnapshot(row.Snapshot, raw); err != nil {
		return result, err
	}
	return MassageBookingPreparation{
		ID:        row.ID,
		Arguments: args,
		Snapshot:  snapshot,
		ExpiresAt: row.ExpiresAt.Time.UTC(),
	}, nil
}

// AdoptOwnMassageCancellation uses the retained owner record; a new pass or
// enabled module is unnecessary. A consumed proposal is recovered through receipts.
func (s *Store) AdoptOwnMassageCancellation(ctx context.Context, scope ProfileScope,
	operation, proposal uuid.UUID, args MassageCancellationArguments,
) (MassageCancellationPreparation, error) {
	var result MassageCancellationPreparation
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageCancellation(args) {
		return result, massage.ErrInvalid
	}
	tx, q, _, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return adoptMassageCancellation(ctx, q, scope, proposal, args)
}

func (s *Store) AdoptMiniAppMassageCancellation(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation, proposal uuid.UUID, args MassageCancellationArguments,
) (MassageCancellationPreparation, error) {
	var result MassageCancellationPreparation
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageCancellation(args) {
		return result, massage.ErrInvalid
	}
	tx, q, _, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return adoptMassageCancellation(ctx, q, scope, proposal, args)
}

func adoptMassageCancellation(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	proposal uuid.UUID, args MassageCancellationArguments,
) (MassageCancellationPreparation, error) {
	var result MassageCancellationPreparation
	row, err := activeMassageProposal(ctx, q, scope, args.EventID, proposal, "cancel", args)
	if err != nil {
		return result, err
	}
	current, err := q.ReadMassageBooking(
		ctx,
		sqlc.ReadMassageBookingParams{ID: args.BookingID, EventID: args.EventID, UserID: scope.UserID},
	)
	if err != nil {
		return result, err
	}
	if current.State != "booked" || current.Version != args.ExpectedVersion {
		return result, ErrConflict
	}
	if err = json.Unmarshal(row.Snapshot, &result.Booking); err != nil {
		return result, err
	}
	if result.Booking.ID != args.BookingID || result.Booking.State != current.State ||
		result.Booking.Version != current.Version {
		return MassageCancellationPreparation{}, ErrConflict
	}
	result.ID, result.Arguments, result.ExpiresAt = row.ID, args, row.ExpiresAt.Time.UTC()
	return result, nil
}

func activeMassageProposal(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event, proposal uuid.UUID, kind string, args any,
) (sqlc.ShuttleMassagePreparation, error) {
	row, err := readMassageApplyProposal(ctx, q, scope, event, proposal, kind, args)
	if err != nil {
		return row, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return row, err
	}
	if row.ConsumedAt.Valid || !row.ExpiresAt.Time.After(now.Time) {
		return row, ErrConflict
	}
	return row, nil
}
