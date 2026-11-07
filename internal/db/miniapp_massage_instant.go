package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/massage"
)

func (s *Store) PrepareMiniAppMassageInstant(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation uuid.UUID, args MassageInstantArguments,
) (MassageInstantPreparation, error) {
	var result MassageInstantPreparation
	if operation == uuid.Nil || !validMassageInstant(args) {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := prepareMassageInstant(ctx, q, scope, event, operation, args, MassageSourceMiniApp)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) AdoptMiniAppMassageInstant(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation, proposal uuid.UUID, args MassageInstantArguments,
) (MassageInstantPreparation, error) {
	var result MassageInstantPreparation
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageInstant(args) {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return adoptMassageInstant(ctx, q, scope, event, proposal, args)
}

func (s *Store) BookMiniAppMassageInstant(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation, proposal uuid.UUID, args MassageInstantArguments,
) (MassageBooking, error) {
	var result MassageBooking
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageInstant(args) {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := bookMassageInstant(ctx, q, scope, event, operation, proposal, args, MassageSourceMiniApp)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}
