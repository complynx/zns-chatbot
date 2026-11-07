package db

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func (s *Store) PrepareOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewDraftChange,
) (OrderReviewWorkspace, error) {
	return s.prepareOrderReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{})
}

func (s *Store) prepareOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewDraftChange,
	origin paymentReviewOrigin,
) (OrderReviewWorkspace, error) {
	normalized, err := normalizeOrderEvidenceReview(args.Review)
	if err != nil || op == uuid.Nil || args.ExpectedRevision < 0 || args.ExpectedRevision == math.MaxInt64 ||
		(args.Language != "" && args.Language != profileLanguageAuto && args.Language != "en" && args.Language != "ru") {
		return OrderReviewWorkspace{}, orders.ErrSelection
	}
	args.Review = normalized
	hash, err := orderHash(args)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	tx, _, err := s.orderReviewTx(ctx, scope, op, args.Review.EventID, args.Review.EvidenceID, origin)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	call, err := q.ReadOrderReviewCall(ctx, sqlc.ReadOrderReviewCallParams{ActorID: scope.UserID, OperationID: op})
	if err == nil {
		if err = matchOrderReviewCall(call, orderReviewSave, origin.source(), hash); err != nil {
			return OrderReviewWorkspace{}, err
		}
		var original OrderReviewWorkspace
		err = json.Unmarshal(call.Result, &original)
		if err == nil {
			err = hydrateOrderReviewWorkspace(ctx, q, scope.UserID, &original)
		}
		return original, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderReviewWorkspace{}, err
	}
	latest, err := q.LatestOrderReviewRevision(
		ctx,
		sqlc.LatestOrderReviewRevisionParams{ActorID: scope.UserID, EvidenceID: args.Review.EvidenceID},
	)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	if latest != args.ExpectedRevision {
		return OrderReviewWorkspace{}, ErrConflict
	}
	id := uuid.New()
	reviewHash, err := orderHash(args.Review)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	prepared, err := prepareOrderEvidenceReview(ctx, q, scope, id, args.Review, reviewHash, args.Language)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	w := OrderReviewWorkspace{
		ID:          id,
		EventID:     args.Review.EventID,
		EvidenceID:  args.Review.EvidenceID,
		Revision:    latest + 1,
		State:       paymentWorkspacePrepared,
		Current:     true,
		Preparation: prepared,
	}
	err = q.InsertOrderReviewWorkspace(
		ctx,
		sqlc.InsertOrderReviewWorkspaceParams{
			ID:         w.ID,
			ActorID:    scope.UserID,
			EventID:    w.EventID,
			EvidenceID: w.EvidenceID,
			Revision:   w.Revision,
			ProposalID: prepared.ID,
		},
	)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	if err = saveOrderReviewCall(ctx, q, scope, op, orderReviewSave, origin, hash, w, w, true, nil); err != nil {
		return OrderReviewWorkspace{}, err
	}
	return w, tx.Commit(ctx)
}

func (s *Store) CurrentOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	event, evidence uuid.UUID,
) (OrderReviewWorkspace, error) {
	return s.currentOrderReviewWorkspace(ctx, scope, event, evidence, paymentReviewOrigin{})
}

func (s *Store) currentOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	event, evidence uuid.UUID,
	origin paymentReviewOrigin,
) (OrderReviewWorkspace, error) {
	tx, err := s.orderEvidenceAuthorityTx(ctx, scope, event, evidence)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = origin.authorize(ctx, tx, scope); err != nil {
		return OrderReviewWorkspace{}, err
	}
	q := s.q.WithTx(tx)
	if _, err = currentAuthorityOrderEvidence(ctx, q, scope, event, evidence); err != nil {
		return OrderReviewWorkspace{}, err
	}
	id, err := q.CurrentOrderReviewWorkspace(
		ctx,
		sqlc.CurrentOrderReviewWorkspaceParams{ActorID: scope.UserID, EventID: event, EvidenceID: evidence},
	)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	return readOrderReviewWorkspace(ctx, q, scope.UserID, id)
}

func (s *Store) CancelOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewWorkspaceChange,
) (OrderReviewWorkspace, error) {
	return s.cancelOrderReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{})
}

func (s *Store) cancelOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewWorkspaceChange,
	origin paymentReviewOrigin,
) (OrderReviewWorkspace, error) {
	if op == uuid.Nil || args.WorkspaceID == uuid.Nil || args.Revision < 1 {
		return OrderReviewWorkspace{}, orders.ErrSelection
	}
	hash, err := orderHash(args)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	tx, w, _, err := s.orderReviewWorkspaceTx(ctx, scope, op, args.WorkspaceID, origin)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	call, err := q.ReadOrderReviewCall(ctx, sqlc.ReadOrderReviewCallParams{ActorID: scope.UserID, OperationID: op})
	if err == nil {
		if err = matchOrderReviewCall(call, orderReviewCancel, origin.source(), hash); err != nil {
			return OrderReviewWorkspace{}, err
		}
		var original OrderReviewWorkspace
		err = json.Unmarshal(call.Result, &original)
		if err == nil {
			err = hydrateOrderReviewWorkspace(ctx, q, scope.UserID, &original)
		}
		return original, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderReviewWorkspace{}, err
	}
	if w.Revision != args.Revision || !w.Current || w.State != paymentWorkspacePrepared {
		return OrderReviewWorkspace{}, ErrConflict
	}
	changed, err := q.CancelOrderReviewWorkspace(
		ctx,
		sqlc.CancelOrderReviewWorkspaceParams{ID: w.ID, ActorID: scope.UserID},
	)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	if changed != 1 {
		return OrderReviewWorkspace{}, ErrConflict
	}
	w.State = paymentWorkspaceCancelled
	if err = saveOrderReviewCall(ctx, q, scope, op, orderReviewCancel, origin, hash, w, w, true, nil); err != nil {
		return OrderReviewWorkspace{}, err
	}
	return w, tx.Commit(ctx)
}
