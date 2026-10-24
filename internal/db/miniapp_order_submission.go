package db

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

type MiniAppOrderSession struct {
	Bearer, Generation [sha256.Size]byte
}

// miniAppOrderTx validates the session inside the business transaction, including
// receipt replays. Its event -> owner -> session order matches agent writes and
// logout. Callers must check IAM before entry and own commit/rollback afterward.
func (s *Store) miniAppOrderTx(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	event, operation uuid.UUID,
) (pgx.Tx, ProfileScope, error) {
	return s.miniAppOrderTxForKind(ctx, deployment, session, event, operation, OrderKindService)
}

func (s *Store) miniAppOrderTxForKind(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, event, operation uuid.UUID, kind OrderKind,
) (pgx.Tx, ProfileScope, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, ProfileScope{}, err
	}
	scope, err := s.lockMiniAppOrderForKind(ctx, tx, deployment, session, event, operation, kind)
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, ProfileScope{}, err
	}
	return tx, scope, nil
}

func (s *Store) lockMiniAppOrderForKind(
	ctx context.Context, tx pgx.Tx, deployment EventScope, session MiniAppOrderSession,
	event, operation uuid.UUID, kind OrderKind,
) (ProfileScope, error) {
	q := s.q.WithTx(tx)
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: deployment.BotID, BearerHash: session.Bearer[:]},
	)
	if err != nil {
		return ProfileScope{}, err
	}
	if err = q.LockEventOperation(ctx, fmt.Sprintf("orders:%s:%s", initial.UserID, operation)); err != nil {
		return ProfileScope{}, err
	}
	_, err = q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: event, BotID: deployment.BotID, OrganizationID: deployment.Organization},
	)
	if err != nil {
		return ProfileScope{}, err
	}
	attempt, err := s.lockMiniAppBearer(ctx, tx, deployment.BotID, session.Bearer, session.Generation)
	if err != nil {
		return ProfileScope{}, err
	}
	if attempt.User.ID != initial.UserID || attempt.User.Organization != deployment.Organization {
		return ProfileScope{}, ErrMiniAppDenied
	}
	scope := ProfileScope{
		BotID:        deployment.BotID,
		Organization: deployment.Organization,
		Subject:      attempt.User.Subject,
		UserID:       attempt.User.ID,
	}
	switch kind {
	case "":
		return scope, authorizeAnyOrderPayment(ctx, q, scope, event)
	case OrderKindService:
		return scope, authorizeOrdersSelection(ctx, tx, scope, event)
	case OrderKindMeal:
		return scope, authorizeMealDraft(ctx, tx, scope, event)
	default:
		return ProfileScope{}, ErrMiniAppDenied
	}
}

func (s *Store) PrepareMiniAppOrderSubmission(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	operation uuid.UUID,
	args OrderSubmission,
) (OrderPreparation, error) {
	return s.prepareMiniAppOrderSubmission(ctx, deployment, session, operation, args, nil, nil)
}

// PrepareMiniAppOrderSubmissionWithContract rechecks the session inside the
// transaction before recording the original operation binding and description.
func (s *Store) PrepareMiniAppOrderSubmissionWithContract(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, operation uuid.UUID, args OrderSubmission,
	contract OrderPreparationContract, describe func(OrderPreparation) (string, error),
) (OrderPreparation, error) {
	return s.prepareMiniAppOrderSubmission(ctx, deployment, session, operation, args, &contract, describe)
}

func (s *Store) prepareMiniAppOrderSubmission(ctx context.Context, deployment EventScope,
	session MiniAppOrderSession, operation uuid.UUID, args OrderSubmission,
	contract *OrderPreparationContract, describe func(OrderPreparation) (string, error),
) (OrderPreparation, error) {
	call, err := orderPrepareCall(operation, args, OrderSourceMiniApp)
	if err != nil {
		return OrderPreparation{}, err
	}
	call.contract, call.describe = contract, describe
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return OrderPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.prepareOrderSubmission(ctx, tx, scope, call, args)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) SubmitMiniAppOrder(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	operation uuid.UUID,
	args OrderSubmission,
	proposal uuid.UUID,
) (OrderSubmissionResult, error) {
	call, err := orderSubmitCall(operation, args, proposal, OrderSourceMiniApp)
	if err != nil {
		return OrderSubmissionResult{}, err
	}
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return OrderSubmissionResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.submitOrder(ctx, tx, scope, call, args, proposal)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) CancelMiniAppOrder(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	operation uuid.UUID,
	args OrderCancellation,
) (SubmittedOrder, error) {
	call, err := orderCancelCall(operation, args, OrderSourceMiniApp)
	if err != nil {
		return SubmittedOrder{}, err
	}
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return SubmittedOrder{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.cancelOrder(ctx, tx, scope, call, args)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}
