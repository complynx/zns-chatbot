package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func normalizeOrderReviewCommit(args OrderReviewCommitChange) (OrderReviewCommitChange, error) {
	normalized, err := normalizeOrderEvidenceReview(args.Review)
	if err != nil || args.WorkspaceID == uuid.Nil || args.Revision < 1 {
		return args, orders.ErrSelection
	}
	args.Review = normalized
	return args, nil
}

func (s *Store) PrepareOrderReviewCommit(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewCommitChange,
) (OrderReviewCommitPreparation, error) {
	args, err := normalizeOrderReviewCommit(args)
	if err != nil || op == uuid.Nil {
		return OrderReviewCommitPreparation{}, orders.ErrSelection
	}
	hash, err := orderHash(args)
	if err != nil {
		return OrderReviewCommitPreparation{}, err
	}
	origin := paymentReviewOrigin{}
	tx, w, _, err := s.orderReviewWorkspaceTx(ctx, scope, op, args.WorkspaceID, origin)
	if err != nil {
		return OrderReviewCommitPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = exactOrderReviewWorkspace(w, args); err != nil {
		return OrderReviewCommitPreparation{}, err
	}
	q := s.q.WithTx(tx)
	call, err := q.ReadOrderReviewCall(ctx, sqlc.ReadOrderReviewCallParams{ActorID: scope.UserID, OperationID: op})
	if err == nil {
		if err = matchOrderReviewCall(call, orderReviewCommit, origin.source(), hash); err != nil {
			return OrderReviewCommitPreparation{}, err
		}
		var result OrderReviewCommitPreparation
		err = json.Unmarshal(call.Document, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderReviewCommitPreparation{}, err
	}
	if err = pendingOrderReviewWorkspace(ctx, q, w, args.Revision); err != nil {
		return OrderReviewCommitPreparation{}, err
	}
	if _, err = orderEvidenceReviewTarget(ctx, q, scope, args.Review); err != nil {
		return OrderReviewCommitPreparation{}, err
	}
	result := OrderReviewCommitPreparation{
		ID:          uuid.New(),
		Arguments:   args,
		Description: w.Preparation.Description,
		ExpiresAt:   w.Preparation.ExpiresAt,
	}
	if err = saveOrderReviewCall(
		ctx,
		q,
		scope,
		op,
		orderReviewCommit,
		origin,
		hash,
		w,
		result,
		false,
		&result,
	); err != nil {
		return OrderReviewCommitPreparation{}, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) CommitOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewCommitChange,
	proposal uuid.UUID,
) (OrderWorkspaceReviewResult, error) {
	return s.commitOrderReviewWorkspace(ctx, scope, op, args, proposal, paymentReviewOrigin{})
}

func (s *Store) commitOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	args OrderReviewCommitChange,
	proposal uuid.UUID,
	origin paymentReviewOrigin,
) (OrderWorkspaceReviewResult, error) {
	args, err := normalizeOrderReviewCommit(args)
	if err != nil || op == uuid.Nil || proposal == uuid.Nil {
		return OrderWorkspaceReviewResult{}, orders.ErrSelection
	}
	// Native confirmation binds the displayed canonical proposal; agent confirmation
	// binds the distinct transport proposal issued by PrepareOrderReviewCommit.
	hashValue := any(args)
	if origin.session != nil {
		hashValue = struct {
			Arguments  OrderReviewCommitChange
			ProposalID uuid.UUID
		}{args, proposal}
	}
	hash, err := orderHash(hashValue)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	tx, w, role, err := s.orderReviewWorkspaceTx(ctx, scope, op, args.WorkspaceID, origin)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = exactOrderReviewWorkspace(w, args); err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	q := s.q.WithTx(tx)
	call, err := orderReviewCommitCall(ctx, q, scope, op, w, args, proposal, hash, origin)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	if len(call.Result) > 0 {
		var prior OrderWorkspaceReviewResult
		err = json.Unmarshal(call.Result, &prior)
		return prior, err
	}
	result, err := applyOrderWorkspaceReview(ctx, q, scope, w, args, role)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	raw, err := orderJSON(result)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	changed, err := q.CompleteOrderReviewCall(
		ctx,
		sqlc.CompleteOrderReviewCallParams{ActorID: scope.UserID, OperationID: op, Result: raw},
	)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	if changed != 1 {
		return OrderWorkspaceReviewResult{}, ErrConflict
	}
	return result, tx.Commit(ctx)
}

func orderReviewCommitCall(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	op uuid.UUID,
	w OrderReviewWorkspace,
	args OrderReviewCommitChange,
	proposal uuid.UUID,
	hash []byte,
	origin paymentReviewOrigin,
) (sqlc.ShuttleOrderReviewCall, error) {
	key := sqlc.ReadOrderReviewCallParams{ActorID: scope.UserID, OperationID: op}
	call, err := q.ReadOrderReviewCall(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) && origin.session != nil {
		if proposal != w.Preparation.ID {
			return call, ErrConflict
		}
		prep := OrderReviewCommitPreparation{
			ID:          proposal,
			Arguments:   args,
			Description: w.Preparation.Description,
			ExpiresAt:   w.Preparation.ExpiresAt,
		}
		if err = saveOrderReviewCall(
			ctx,
			q,
			scope,
			op,
			orderReviewCommit,
			origin,
			hash,
			w,
			prep,
			false,
			&prep,
		); err != nil {
			return call, err
		}
		call, err = q.ReadOrderReviewCall(ctx, key)
	}
	if err != nil {
		return call, err
	}
	if err = matchOrderReviewCall(call, orderReviewCommit, origin.source(), hash); err != nil {
		return call, err
	}
	if call.WorkspaceID != w.ID || !call.ProposalID.Valid || uuid.UUID(call.ProposalID.Bytes) != proposal {
		return call, ErrConflict
	}
	return call, nil
}

// The caller owns the full effect + workspace + transport receipt transaction.
// Canonical completion is recovered before current/expiry tests, after authority.
func applyOrderWorkspaceReview(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	w OrderReviewWorkspace,
	args OrderReviewCommitChange,
	role string,
) (OrderWorkspaceReviewResult, error) {
	hash, requestHash, err := orderReviewCanonicalHashes(w, args.Review)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	prior, found, err := readOrderEvidenceReceipt(ctx, q, scope.UserID, w.EventID, w.ID, paymentReview, requestHash)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	if found {
		return OrderWorkspaceReviewResult{CanonicalOperationID: w.ID, Evidence: prior}, nil
	}
	if err = pendingOrderReviewWorkspace(ctx, q, w, args.Revision); err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	result, _, err := reviewOrderEvidence(ctx, q, scope, w.ID, w.Preparation.ID, args.Review, hash, requestHash, role)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	changed, err := q.CompleteOrderReviewWorkspace(
		ctx,
		sqlc.CompleteOrderReviewWorkspaceParams{ID: w.ID, ActorID: scope.UserID},
	)
	if err != nil {
		return OrderWorkspaceReviewResult{}, err
	}
	if changed != 1 {
		return OrderWorkspaceReviewResult{}, ErrConflict
	}
	return OrderWorkspaceReviewResult{CanonicalOperationID: w.ID, Evidence: result}, nil
}

func orderReviewCanonicalHashes(w OrderReviewWorkspace, review OrderEvidenceReview) ([]byte, []byte, error) {
	hash, err := orderHash(review)
	if err != nil {
		return nil, nil, err
	}
	requestHash, err := orderHash(struct {
		Arguments  OrderEvidenceReview
		ProposalID uuid.UUID
	}{review, w.Preparation.ID})
	return hash, requestHash, err
}
