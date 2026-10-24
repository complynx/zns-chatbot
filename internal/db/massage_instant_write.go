package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// PrepareOwnMassageInstant freezes a specialist's self reservation without
// creating a booking or holding capacity. Retries retain the original expiry.
func (s *Store) PrepareOwnMassageInstant(ctx context.Context, scope ProfileScope,
	operation uuid.UUID, args MassageInstantArguments,
) (MassageInstantPreparation, error) {
	var result MassageInstantPreparation
	if operation == uuid.Nil || !validMassageInstant(args) {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := prepareMassageInstant(ctx, q, scope, event, operation, args, MassageSourceAgent)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func prepareMassageInstant(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, operation uuid.UUID, args MassageInstantArguments, source MassageBookingSource,
) (MassageInstantPreparation, bool, error) {
	var result MassageInstantPreparation
	hash, err := massageRequestHash(massageInstantKind, args, uuid.Nil)
	if err != nil {
		return result, false, err
	}
	row, err := q.ReadMassagePreparationByOperation(ctx, sqlc.ReadMassagePreparationByOperationParams{
		UserID: scope.UserID, OperationID: operation,
	})
	if err == nil {
		if !massageProposalValid(row, massageInstantKind, hash) {
			return result, false, ErrConflict
		}
		err = json.Unmarshal(row.Snapshot, &result.Snapshot)
		result.ID, result.Arguments, result.ExpiresAt = row.ID, args, row.ExpiresAt.Time.UTC()
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, false, err
	}
	snapshot, err := currentMassageInstantSnapshot(ctx, q, scope, event, args, nil)
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
	row, err = q.InsertMassagePreparation(ctx, sqlc.InsertMassagePreparationParams{
		ID: uuid.New(), EventID: args.EventID, UserID: scope.UserID, OperationID: operation,
		Kind: massageInstantKind, Source: string(source), RequestHash: hash,
		Arguments: rawArgs, Snapshot: rawSnapshot,
	})
	if err != nil {
		return result, false, err
	}
	return MassageInstantPreparation{
		ID: row.ID, Arguments: args, Snapshot: snapshot, ExpiresAt: row.ExpiresAt.Time.UTC(),
	}, false, nil
}

// BookOwnMassageInstant applies only the frozen self reservation. Historical
// receipts survive later revocation; new writes recheck the current staff grant.
func (s *Store) BookOwnMassageInstant(ctx context.Context, scope ProfileScope,
	operation, proposal uuid.UUID, args MassageInstantArguments, source MassageBookingSource,
) (MassageBooking, error) {
	var result MassageBooking
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageInstant(args) || !massageSourceValid(source) {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := bookMassageInstant(ctx, q, scope, event, operation, proposal, args, source)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func bookMassageInstant(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, operation, proposal uuid.UUID, args MassageInstantArguments, source MassageBookingSource,
) (MassageBooking, bool, error) {
	var result MassageBooking
	hash, err := massageRequestHash(massageInstantKind, args, proposal)
	if err != nil {
		return result, false, err
	}
	if prior, replayed, priorErr := massagePriorResult(
		ctx, q, scope, args.EventID, operation, massageInstantKind, hash,
	); priorErr != nil || replayed {
		return prior, replayed, priorErr
	}
	prepared, err := readMassageApplyProposal(ctx, q, scope, args.EventID, proposal, massageInstantKind, args)
	if err != nil {
		return result, false, err
	}
	if prepared.ConsumedAt.Valid {
		result, err = massageConsumedResult(ctx, q, scope, operation, massageInstantKind, hash, prepared, source)
		return result, false, err
	}
	snapshot, raw, err := revalidateMassageInstant(ctx, q, scope, event, args, prepared)
	if err != nil {
		return result, false, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, false, err
	}
	if !prepared.ExpiresAt.Time.After(now.Time) ||
		snapshot.Terms.StartsAt.Before(now.Time.Add(-massage.SpecialistBookingGrace)) {
		return result, false, ErrConflict
	}
	result, err = insertMassageBooking(ctx, q, scope, args.EventID, proposal, snapshot, raw, source)
	if err == nil {
		err = consumeMassageProposal(ctx, q, proposal, result)
	}
	if err == nil {
		err = insertMassageReceipt(ctx, q, scope, operation, massageInstantKind, hash, proposal, result, source)
	}
	return result, false, err
}
