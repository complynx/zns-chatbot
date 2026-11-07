package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderReviewSuperseded = "superseded"
const orderReviewExpired = "expired"

func (s *Store) OrderReviewWorkspaceByID(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
) (OrderReviewWorkspace, error) {
	return s.orderReviewWorkspaceByID(ctx, scope, id, paymentReviewOrigin{})
}

func (s *Store) orderReviewWorkspaceByID(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	origin paymentReviewOrigin,
) (OrderReviewWorkspace, error) {
	if id == uuid.Nil {
		return OrderReviewWorkspace{}, orders.ErrSelection
	}
	tx, w, _, err := s.orderReviewWorkspaceTx(ctx, scope, uuid.New(), id, origin)
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return w, nil
}

func (s *Store) OrderReviewOperationByID(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
) (OrderReviewReceipt, error) {
	return s.orderReviewOperationByID(ctx, scope, op, paymentReviewOrigin{})
}

// Resolve a private receipt without holding actor locks across event acquisition.
func (s *Store) orderReviewOperationByID(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	origin paymentReviewOrigin,
) (OrderReviewReceipt, error) {
	if op == uuid.Nil {
		return OrderReviewReceipt{}, orders.ErrSelection
	}
	initial, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderReviewReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, initial)
	if err = lockProfileOwner(ctx, initial, orderEvidenceProfile(scope), false); err != nil {
		return OrderReviewReceipt{}, err
	}
	key := sqlc.ReadOrderReviewCallParams{ActorID: scope.UserID, OperationID: op}
	call, err := s.q.WithTx(initial).ReadOrderReviewCall(ctx, key)
	if err != nil {
		return OrderReviewReceipt{}, err
	}
	rollbackLegacyArchive(ctx, initial)
	tx, w, _, err := s.orderReviewWorkspaceTx(ctx, scope, op, call.WorkspaceID, origin)
	if err != nil {
		return OrderReviewReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	call, err = s.q.WithTx(tx).ReadOrderReviewCall(ctx, key)
	if err != nil {
		return OrderReviewReceipt{}, err
	}
	result := OrderReviewReceipt{OperationID: op, Kind: call.Kind, Source: call.Source, Completed: len(call.Result) > 0}
	switch call.Kind {
	case orderReviewSave, orderReviewCancel:
		result.Workspace = &OrderReviewWorkspace{}
		err = json.Unmarshal(call.Result, result.Workspace)
		if err == nil {
			err = hydrateOrderReviewWorkspace(ctx, s.q.WithTx(tx), scope.UserID, result.Workspace)
		}
	case orderReviewCommit:
		result.Preparation = &OrderReviewCommitPreparation{}
		err = json.Unmarshal(call.Document, result.Preparation)
		if err == nil {
			err = hydrateOrderReviewCommit(w, result.Preparation)
		}
		if err == nil && len(call.Result) > 0 {
			result.Review = &OrderWorkspaceReviewResult{}
			err = json.Unmarshal(call.Result, result.Review)
		}
		if err == nil && len(call.Result) == 0 {
			result, err = resolvePendingOrderReview(ctx, s.q.WithTx(tx), scope, w, result)
		}
	default:
		err = ErrConflict
	}
	return result, err
}

// Another approved interface may have completed the canonical decision while
// this transport still has a pending call. Reconcile read-only before expiry.
func resolvePendingOrderReview(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	w OrderReviewWorkspace,
	result OrderReviewReceipt,
) (OrderReviewReceipt, error) {
	args := result.Preparation.Arguments
	if err := exactOrderReviewWorkspace(w, args); err != nil {
		return result, err
	}
	_, requestHash, err := orderReviewCanonicalHashes(w, args.Review)
	if err != nil {
		return result, err
	}
	canonical, found, err := readOrderEvidenceReceipt(ctx, q, scope.UserID, w.EventID, w.ID, paymentReview, requestHash)
	if err != nil {
		return result, err
	}
	if found {
		result.Completed = true
		result.Review = &OrderWorkspaceReviewResult{CanonicalOperationID: w.ID, Evidence: canonical}
		return result, nil
	}
	switch {
	case !w.Current:
		result.UnavailableReason = orderReviewSuperseded
	case w.State == paymentWorkspaceCancelled:
		result.UnavailableReason = paymentWorkspaceCancelled
	case w.State != paymentWorkspacePrepared:
		result.UnavailableReason = "workspace_unavailable"
	case w.Preparation.Description == "":
		result.UnavailableReason = "missing_consent_summary"
	default:
		now, nowErr := q.RegistrationNow(ctx)
		if nowErr != nil {
			return result, nowErr
		}
		if !now.Time.Before(w.Preparation.ExpiresAt) {
			result.UnavailableReason = orderReviewExpired
			return result, nil
		}
		row, readErr := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: w.EvidenceID, EventID: w.EventID})
		if readErr != nil {
			return result, readErr
		}
		if row.Version != args.Review.ExpectedVersion {
			result.UnavailableReason = "evidence_decided"
		}
	}
	return result, nil
}
