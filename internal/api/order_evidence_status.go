package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) orderReviewStatusReceipt(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (db.OrderReviewReceipt, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return db.OrderReviewReceipt{}, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return db.OrderReviewReceipt{}, huma.Error400BadRequest("nonzero operation UUID required")
	}
	return s.store.OrderReviewOperationByID(ctx, scope, op)
}

func orderEvidenceStatusResult[T any](operation string, result *T, err error) (*fileEnvelope, error) {
	out := &paymentWriteResponse[T]{}
	out.Body.OperationID = operation
	if errors.Is(err, pgx.ErrNoRows) || err == nil && result == nil {
		out.Body.Status = agentNotFound
		return boundedOrderEvidenceJSON(out.Body)
	}
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, result
	return boundedOrderEvidenceJSON(out.Body)
}

func (s *Server) agentOrderEvidenceSubmitStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	receipt, err := s.store.OrderEvidenceOperationByID(ctx, scope, op, db.OrderEvidenceSubmissionReceipt)
	if err != nil {
		return orderEvidenceStatusResult[db.OrderPaymentEvidence](in.Body.OperationID, nil, err)
	}
	return orderEvidenceStatusResult(in.Body.OperationID, &receipt.Result, nil)
}

func (s *Server) agentOrderReviewSaveStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*fileEnvelope, error) {
	receipt, err := s.orderReviewStatusReceipt(ctx, in)
	if receipt.Kind != "save" {
		receipt.Workspace = nil
	}
	return orderEvidenceStatusResult(in.Body.OperationID, receipt.Workspace, err)
}

func (s *Server) agentOrderReviewCancelStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*fileEnvelope, error) {
	receipt, err := s.orderReviewStatusReceipt(ctx, in)
	if receipt.Kind != orderReviewCancelKind {
		receipt.Workspace = nil
	}
	return orderEvidenceStatusResult(in.Body.OperationID, receipt.Workspace, err)
}

func (s *Server) agentOrderReviewCommitStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*fileEnvelope, error) {
	receipt, err := s.orderReviewStatusReceipt(ctx, in)
	if err != nil || receipt.Kind != "commit" {
		return orderEvidenceStatusResult[db.OrderWorkspaceReviewResult](in.Body.OperationID, nil, err)
	}
	if receipt.Completed {
		return orderEvidenceStatusResult(in.Body.OperationID, receipt.Review, nil)
	}
	out := &paymentWriteResponse[db.OrderWorkspaceReviewResult]{}
	out.Body.OperationID = in.Body.OperationID
	out.Body.Status = agentPending
	if receipt.UnavailableReason != "" {
		out.Body.Status = "cancelled"
		out.Body.Error = &agentRegistrationError{
			Code:           receipt.UnavailableReason,
			Message:        "prepared order review cannot execute",
			Classification: agentTerminalRejection,
		}
	}
	return boundedOrderEvidenceJSON(out.Body)
}
