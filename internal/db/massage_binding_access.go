package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func (s *Store) massageBindingTx(ctx context.Context, scope ProfileScope, operation, eventID uuid.UUID,
	session *MiniAppMassageSession,
) (pgx.Tx, *sqlc.Queries, sqlc.ShuttleEvent, error) {
	if operation == uuid.Nil {
		return nil, nil, sqlc.ShuttleEvent{}, massage.ErrInvalid
	}
	if session == nil {
		return s.massageEventWriteTx(ctx, scope, operation, eventID, "massage-binding")
	}
	tx, q, event, actor, err := s.miniAppMassageOperationTx(
		ctx,
		EventScope{BotID: scope.BotID, Organization: scope.Organization},
		*session,
		eventID,
		operation,
		"massage-binding",
	)
	if err == nil && (actor.UserID != scope.UserID || actor.Subject != scope.Subject) {
		rollbackLegacyArchive(ctx, tx)
		return nil, nil, event, ErrMiniAppDenied
	}
	return tx, q, event, err
}

func (s *Store) lockMassageBindingActor(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	session *MiniAppMassageSession,
) error {
	actor, err := s.lockMassageStaffActor(ctx, tx, scope, session)
	if err == nil && (actor.UserID != scope.UserID || actor.Subject != scope.Subject) {
		return ErrMiniAppDenied
	}
	return err
}

// PrepareMiniAppMassageBinding requires current global IAM administrator authorization
// at the API boundary. Native calls also validate the same actor's session in TX.
func (s *Store) PrepareMiniAppMassageBinding(
	ctx context.Context,
	scope ProfileScope,
	session MiniAppMassageSession,
	operation uuid.UUID,
	args MassageBindingChange,
) (MassageBindingPreparation, error) {
	return s.prepareMassageBinding(ctx, scope, operation, uuid.Nil, args, &session)
}

func (s *Store) ChangeMiniAppMassageBinding(
	ctx context.Context,
	scope ProfileScope,
	session MiniAppMassageSession,
	operation, proposal uuid.UUID,
	args MassageBindingChange,
	checkIdentity func(context.Context) error,
) (MassageSpecialistBinding, error) {
	return s.changeMassageBinding(ctx, scope, operation, proposal, args, checkIdentity, &session)
}

func (s *Store) MiniAppMassageBindingOperation(
	ctx context.Context,
	scope ProfileScope,
	session MiniAppMassageSession,
	operation uuid.UUID,
) (MassageSpecialistBinding, error) {
	return s.massageBindingOperation(ctx, scope, operation, &session)
}

// AdoptMiniAppMassageBinding continues the same pending proposal under a current native session.
func (s *Store) AdoptMiniAppMassageBinding(
	ctx context.Context, scope ProfileScope, session MiniAppMassageSession,
	operation, proposal uuid.UUID, args MassageBindingChange,
) (MassageBindingPreparation, error) {
	if proposal == uuid.Nil {
		return MassageBindingPreparation{}, massage.ErrInvalid
	}
	return s.prepareMassageBinding(ctx, scope, operation, proposal, args, &session)
}
