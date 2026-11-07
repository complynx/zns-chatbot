package db

import (
	"context"

	"github.com/google/uuid"
)

// PrepareMiniAppOrderReviewWorkspace checks the session after orders event/actor
// locks. It does not require the registrations module.
func (s *Store) PrepareMiniAppOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentSession,
	op uuid.UUID,
	args OrderReviewDraftChange,
) (OrderReviewWorkspace, error) {
	return s.prepareOrderReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{session: &session})
}

func (s *Store) CurrentMiniAppOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentSession,
	event, evidence uuid.UUID,
) (OrderReviewWorkspace, error) {
	return s.currentOrderReviewWorkspace(ctx, scope, event, evidence, paymentReviewOrigin{session: &session})
}

func (s *Store) CancelMiniAppOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentSession,
	op uuid.UUID,
	args OrderReviewWorkspaceChange,
) (OrderReviewWorkspace, error) {
	return s.cancelOrderReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{session: &session})
}

// CommitMiniAppOrderReviewWorkspace requires the canonical proposal displayed
// with this exact revision, and rechecks the session inside the transaction.
func (s *Store) CommitMiniAppOrderReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentSession,
	op uuid.UUID,
	args OrderReviewCommitChange,
	canonicalProposalID uuid.UUID,
) (OrderWorkspaceReviewResult, error) {
	return s.commitOrderReviewWorkspace(
		ctx,
		scope,
		op,
		args,
		canonicalProposalID,
		paymentReviewOrigin{session: &session},
	)
}

func (s *Store) MiniAppOrderReviewOperationByID(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPaymentSession,
	op uuid.UUID,
) (OrderReviewReceipt, error) {
	return s.orderReviewOperationByID(ctx, scope, op, paymentReviewOrigin{session: &session})
}
