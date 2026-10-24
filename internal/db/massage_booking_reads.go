package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// massageOwnerReadTx authorizes historical own records without requiring a new
// pass or enabled booking module. Reads share one consistent snapshot/connection.
func (s *Store) massageOwnerReadTx(
	ctx context.Context,
	scope ProfileScope,
	eventID uuid.UUID,
) (pgx.Tx, *sqlc.Queries, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, nil, err
	}
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	if eventID != uuid.Nil {
		_, err = q.GetEvent(
			ctx,
			sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
		if err != nil {
			rollbackLegacyArchive(ctx, tx)
			return nil, nil, err
		}
	}
	return tx, q, nil
}

func (s *Store) OwnMassageBooking(
	ctx context.Context,
	scope ProfileScope,
	eventID, bookingID uuid.UUID,
) (MassageBooking, error) {
	if eventID == uuid.Nil || bookingID == uuid.Nil {
		return MassageBooking{}, massage.ErrInvalid
	}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, eventID)
	if err != nil {
		return MassageBooking{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.ReadMassageBooking(
		ctx,
		sqlc.ReadMassageBookingParams{ID: bookingID, EventID: eventID, UserID: scope.UserID},
	)
	if err != nil {
		return MassageBooking{}, err
	}
	return massageBookingRecord(row)
}

func (s *Store) OwnMassageBookings(
	ctx context.Context,
	scope ProfileScope,
	eventID, after uuid.UUID,
	limit int,
) (MassageBookingPage, error) {
	result := MassageBookingPage{Bookings: []MassageBooking{}}
	if eventID == uuid.Nil || limit < 0 || limit > 20 {
		return result, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, eventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnMassageBookings(ctx, q, scope, eventID, after, limit)
}

func readOwnMassageBookings(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	eventID, after uuid.UUID, limit int,
) (MassageBookingPage, error) {
	if limit < 1 || limit > 20 {
		return MassageBookingPage{}, massage.ErrInvalid
	}
	result := MassageBookingPage{Bookings: []MassageBooking{}}
	rows, err := q.ListOwnMassageBookings(
		ctx,
		sqlc.ListOwnMassageBookingsParams{EventID: eventID, UserID: scope.UserID, ID: after, Limit: int32(limit + 1)},
	)
	if err != nil {
		return result, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[len(rows)-1].ID
		result.NextAfter = &next
	}
	for _, row := range rows {
		booking, decodeErr := massageBookingRecord(row)
		if decodeErr != nil {
			return result, decodeErr
		}
		result.Bookings = append(result.Bookings, booking)
	}
	return result, nil
}

// OwnMassageOperation returns the original historical effect, never a rewritten
// current booking. Missing status does not authorize another operation.
func (s *Store) OwnMassageOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MassageBooking, error) {
	receipt, err := s.OwnMassageOperationReceipt(ctx, scope, operation)
	return receipt.Result, err
}

func (s *Store) OwnMassageOperationReceipt(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (MassageOperationReceipt, error) {
	var result MassageOperationReceipt
	if operation == uuid.Nil {
		return result, massage.ErrInvalid
	}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, uuid.Nil)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOwnMassageOperation(ctx, q, scope, operation)
}

func readOwnMassageOperation(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	operation uuid.UUID,
) (MassageOperationReceipt, error) {
	var result MassageOperationReceipt
	row, err := q.ReadMassageOperation(
		ctx,
		sqlc.ReadMassageOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return result, err
	}
	if _, err = q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: row.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return result, err
	}
	result = MassageOperationReceipt{
		OperationID: row.OperationID, EventID: row.EventID, Kind: row.Kind,
		Source: MassageBookingSource(row.Source), ProposalID: row.PreparationID,
		BookingID: row.BookingID, RecordedAt: row.CreatedAt.Time.UTC(),
	}
	err = json.Unmarshal(row.Result, &result.Result)
	return result, err
}
