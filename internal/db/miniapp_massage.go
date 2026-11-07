package db

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

type MiniAppMassageSession struct{ Bearer, Generation [sha256.Size]byte }

// miniAppMassageTx uses operation -> event -> owner -> session ordering. Current
// session validation precedes receipts and commits with business changes on one
// connection. The caller must check IAM before entry and own commit/rollback.
func (s *Store) miniAppMassageTx(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, operation uuid.UUID,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, ProfileScope, error) {
	return s.miniAppMassageOperationTx(ctx, deployment, session, eventID, operation, "massage-booking")
}

func (s *Store) miniAppMassageOperationTx(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, operation uuid.UUID, kind string,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, ProfileScope, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, sqlc.ShuttleEvent{}, ProfileScope{}, err
	}
	q := s.q.WithTx(tx)
	scope, event, err := s.lockMiniAppMassage(ctx, tx, deployment, session, eventID, operation, kind)
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, event, scope, err
	}
	return tx, q, event, scope, nil
}

func (s *Store) lockMiniAppMassage(ctx context.Context, tx pgx.Tx, deployment EventScope,
	session MiniAppMassageSession, eventID, operation uuid.UUID, kind string,
) (ProfileScope, sqlc.ShuttleEvent, error) {
	q := s.q.WithTx(tx)
	var scope ProfileScope
	var event sqlc.ShuttleEvent
	initial, err := q.FindMiniAppBearer(ctx, sqlc.FindMiniAppBearerParams{
		BotID: deployment.BotID, BearerHash: session.Bearer[:],
	})
	if err != nil {
		return scope, event, err
	}
	if operation != uuid.Nil {
		if err = q.LockEventOperation(
			ctx,
			fmt.Sprintf("%s:%s:%s", kind, initial.UserID, operation),
		); err != nil {
			return scope, event, err
		}
	}
	event, err = q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
		ID: eventID, BotID: deployment.BotID, OrganizationID: deployment.Organization,
	})
	if err != nil {
		return scope, event, err
	}
	attempt, err := s.lockMiniAppBearer(ctx, tx, deployment.BotID, session.Bearer, session.Generation)
	if err != nil {
		return scope, event, err
	}
	if attempt.User.ID != initial.UserID || attempt.User.Organization != deployment.Organization {
		return scope, event, ErrMiniAppDenied
	}
	scope = ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization,
		Subject: attempt.User.Subject, UserID: attempt.User.ID}
	return scope, event, nil
}

func (s *Store) PrepareMiniAppMassageBooking(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation uuid.UUID, args MassageBookingArguments,
) (MassageBookingPreparation, error) {
	var result MassageBookingPreparation
	args, err := normalizeMassageBooking(args)
	if err != nil || operation == uuid.Nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.prepareOwnMassageBooking(ctx, q, scope, event, operation, args, MassageSourceMiniApp)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) BookMiniAppMassage(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation, proposal uuid.UUID, args MassageBookingArguments,
) (MassageBooking, error) {
	var result MassageBooking
	args, err := normalizeMassageBooking(args)
	if err != nil || operation == uuid.Nil || proposal == uuid.Nil {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.bookOwnMassage(ctx, q, scope, event, operation, proposal, args, MassageSourceMiniApp)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) PrepareMiniAppMassageCancellation(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation uuid.UUID, args MassageCancellationArguments,
) (MassageCancellationPreparation, error) {
	var result MassageCancellationPreparation
	if operation == uuid.Nil || !validMassageCancellation(args) {
		return result, massage.ErrInvalid
	}
	tx, q, _, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.prepareOwnMassageCancellation(ctx, q, scope, operation, args, MassageSourceMiniApp)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) CancelMiniAppMassage(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, operation, proposal uuid.UUID, args MassageCancellationArguments,
) (MassageBooking, error) {
	var result MassageBooking
	if operation == uuid.Nil || proposal == uuid.Nil || !validMassageCancellation(args) {
		return result, massage.ErrInvalid
	}
	tx, q, _, scope, err := s.miniAppMassageTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.cancelOwnMassage(ctx, q, scope, operation, proposal, args, MassageSourceMiniApp)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}
