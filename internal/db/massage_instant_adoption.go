package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// AdoptOwnMassageInstant revalidates the retained choice without moving its
// start or renewing consent. Application still requires explicit confirmation.
func (s *Store) AdoptOwnMassageInstant(ctx context.Context, scope ProfileScope,
	operation, proposal uuid.UUID, args MassageInstantArguments,
) (MassageInstantPreparation, error) {
	var result MassageInstantPreparation
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageInstant(args) {
		return result, massage.ErrInvalid
	}
	tx, q, event, err := s.massageBookingTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return adoptMassageInstant(ctx, q, scope, event, proposal, args)
}

func adoptMassageInstant(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, proposal uuid.UUID, args MassageInstantArguments,
) (MassageInstantPreparation, error) {
	var result MassageInstantPreparation
	row, err := activeMassageProposal(ctx, q, scope, args.EventID, proposal, massageInstantKind, args)
	if err != nil {
		return result, err
	}
	snapshot, _, err := revalidateMassageInstant(ctx, q, scope, event, args, row)
	if err != nil {
		return result, err
	}
	return MassageInstantPreparation{
		ID: row.ID, Arguments: args, Snapshot: snapshot, ExpiresAt: row.ExpiresAt.Time.UTC(),
	}, nil
}
