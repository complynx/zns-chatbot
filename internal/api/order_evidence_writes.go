package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type orderReviewWorkspaceView struct {
	Workspace *db.OrderReviewWorkspace `json:"workspace"`
}
type orderReviewOperationView struct {
	Receipt *db.OrderReviewReceipt `json:"receipt"`
}
type orderReviewOperationArguments struct {
	OperationID uuid.UUID `json:"operation_id"`
}

func (s *Server) registerOrderEvidenceWrites(api huma.API) {
	huma.Register(api, orderEvidenceRoute("submit"), s.agentSubmitOrderEvidence)
	huma.Register(api, orderEvidenceRoute("submit/status"), s.agentOrderEvidenceSubmitStatus)
	huma.Register(api, orderEvidenceRoute("review_workspace"), s.agentOrderReviewWorkspace)
	huma.Register(api, orderEvidenceRoute("review_operation"), s.agentOrderReviewOperation)
	huma.Register(api, orderEvidenceRoute("save_review"), s.agentSaveOrderReview)
	huma.Register(api, orderEvidenceRoute("save_review/status"), s.agentOrderReviewSaveStatus)
	huma.Register(api, orderEvidenceRoute("cancel_review"), s.agentCancelOrderReview)
	huma.Register(api, orderEvidenceRoute("cancel_review/status"), s.agentOrderReviewCancelStatus)
	huma.Register(api, orderEvidenceRoute("review_commit/prepare"), s.agentPrepareOrderReviewCommit)
	huma.Register(api, orderEvidenceRoute("review_commit"), s.agentCommitOrderReview)
	huma.Register(api, orderEvidenceRoute("review_commit/status"), s.agentOrderReviewCommitStatus)
}

func orderEvidenceWriteOperation[T any](in *paymentWriteRequest[T], exact bool) (uuid.UUID, uuid.UUID, error) {
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return uuid.Nil, uuid.Nil, huma.Error400BadRequest(
			"matching nonzero operation UUID and idempotency key required",
		)
	}
	confirmation := in.Body.Confirmation
	if !exact {
		if confirmation != nil && (confirmation.Mode != confirmationNone || confirmation.Preconditions != nil) {
			return uuid.Nil, uuid.Nil, huma.Error400BadRequest("no-confirmation mode required")
		}
		return op, uuid.Nil, nil
	}
	if confirmation == nil || confirmation.Mode != assignmentConfirmationMode || confirmation.Preconditions == nil ||
		confirmation.Preconditions.ProposalID == uuid.Nil {
		return uuid.Nil, uuid.Nil, huma.Error400BadRequest("exact proposal confirmation required")
	}
	return op, confirmation.Preconditions.ProposalID, nil
}

func orderEvidenceWriteResult[T any](operation string, result T, err error) (*fileEnvelope, error) {
	out := &paymentWriteResponse[T]{}
	out.Body.OperationID = operation
	if err == nil {
		out.Body.Status, out.Body.Result = agentSucceeded, &result
		return boundedOrderEvidenceJSON(out.Body)
	}
	mapped := orderEvidenceError(err)
	status, ok := errors.AsType[huma.StatusError](mapped)
	if !ok || status.GetStatus() >= http.StatusInternalServerError {
		return nil, mapped
	}
	out.Body.Status = agentRejected
	out.Body.Error = &agentRegistrationError{
		Code:           "order_evidence_rejected",
		Message:        "current access, target, revision, proposal or operation rejected",
		Classification: agentTerminalRejection,
	}
	return boundedOrderEvidenceJSON(out.Body)
}

func (s *Server) agentSubmitOrderEvidence(
	ctx context.Context,
	in *paymentWriteRequest[db.OrderEvidenceSubmission],
) (*fileEnvelope, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, _, err := orderEvidenceWriteOperation(in, false)
	if err != nil {
		return nil, err
	}
	result, err := s.store.SubmitOwnOrderEvidence(ctx, scope, op, in.Body.Arguments)
	return orderEvidenceWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) agentSaveOrderReview(
	ctx context.Context,
	in *paymentWriteRequest[db.OrderReviewDraftChange],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, _, err := orderEvidenceWriteOperation(in, false)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PrepareOrderReviewWorkspace(ctx, scope, op, in.Body.Arguments)
	return orderEvidenceWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) agentCancelOrderReview(
	ctx context.Context,
	in *paymentWriteRequest[db.OrderReviewWorkspaceChange],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, _, err := orderEvidenceWriteOperation(in, false)
	if err != nil {
		return nil, err
	}
	result, err := s.store.CancelOrderReviewWorkspace(ctx, scope, op, in.Body.Arguments)
	return orderEvidenceWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) agentPrepareOrderReviewCommit(
	ctx context.Context,
	in *agentReadRequest[db.OrderReviewCommitChange],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	prepared, err := s.store.PrepareOrderReviewCommit(ctx, scope, op, in.Body.Arguments)
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	if prepared.Description == "" {
		return nil, huma.Error409Conflict("fresh shared review summary required")
	}
	out := struct {
		Status        string                     `json:"status"`
		OperationID   string                     `json:"operation_id"`
		Arguments     db.OrderReviewCommitChange `json:"arguments"`
		Preconditions assignmentPreconditions    `json:"preconditions"`
		Description   string                     `json:"description"`
		ExpiresAt     time.Time                  `json:"expires_at"`
	}{agentPrepared, in.Body.OperationID, prepared.Arguments, assignmentPreconditions{ProposalID: prepared.ID}, prepared.Description, prepared.ExpiresAt}
	return boundedOrderEvidenceJSON(out)
}

func (s *Server) agentCommitOrderReview(
	ctx context.Context,
	in *paymentWriteRequest[db.OrderReviewCommitChange],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, proposal, err := orderEvidenceWriteOperation(in, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.CommitOrderReviewWorkspace(ctx, scope, op, in.Body.Arguments, proposal)
	return orderEvidenceWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) agentOrderReviewWorkspace(
	ctx context.Context,
	in *agentReadRequest[paymentEvidenceArguments],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	w, err := s.store.CurrentOrderReviewWorkspace(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.EvidenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderEvidenceReadResult(in.Body.OperationID, orderReviewWorkspaceView{})
	}
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	return orderEvidenceReadResult(in.Body.OperationID, orderReviewWorkspaceView{Workspace: &w})
}

func (s *Server) agentOrderReviewOperation(
	ctx context.Context,
	in *agentReadRequest[orderReviewOperationArguments],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.OrderReviewOperationByID(ctx, scope, in.Body.Arguments.OperationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderEvidenceReadResult(in.Body.OperationID, orderReviewOperationView{})
	}
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	return orderEvidenceReadResult(in.Body.OperationID, orderReviewOperationView{Receipt: &receipt})
}
