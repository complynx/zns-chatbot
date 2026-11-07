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

type paymentWorkspaceView struct {
	Workspace *db.PaymentReviewWorkspace `json:"workspace"`
}

type paymentWorkspaceOperationView struct {
	Receipt *db.PaymentReviewWorkspaceReceipt `json:"receipt"`
}

type paymentWorkspaceOperationArguments struct {
	OperationID string `json:"operation_id" minLength:"1" maxLength:"128"`
}

type paymentWorkspacePreparationResponse struct {
	Body struct {
		Status        string                       `json:"status"`
		OperationID   string                       `json:"operation_id"`
		Arguments     db.PaymentReviewCommitChange `json:"arguments"`
		Preconditions assignmentPreconditions      `json:"preconditions"`
		Description   string                       `json:"description"`
		ExpiresAt     time.Time                    `json:"expires_at"`
	}
}

func (s *Server) registerPaymentWorkspaces(api huma.API) {
	huma.Register(api, paymentWorkspaceRoute("review_workspace"), s.agentPaymentWorkspace)
	huma.Register(api, paymentWorkspaceRoute("review_operation"), s.agentPaymentWorkspaceOperation)
	huma.Register(api, paymentWorkspaceRoute("save_review"), s.agentSavePaymentReview)
	huma.Register(api, paymentWorkspaceRoute("save_review/status"), s.paymentWorkspaceSaveStatus)
	huma.Register(api, paymentWorkspaceRoute("cancel_review"), s.agentCancelPaymentReview)
	huma.Register(api, paymentWorkspaceRoute("cancel_review/status"), s.paymentWorkspaceCancelStatus)
	huma.Register(api, paymentWorkspaceRoute("review_commit/prepare"), s.preparePaymentWorkspaceCommit)
	huma.Register(api, paymentWorkspaceRoute("review_commit"), s.commitPaymentWorkspace)
	huma.Register(api, paymentWorkspaceRoute("review_commit/status"), s.paymentWorkspaceCommitStatus)
}

func paymentWorkspaceRoute(path string) huma.Operation {
	return huma.Operation{
		OperationID:  "agent-payment-" + path,
		Method:       http.MethodPost,
		Path:         paymentPath + path,
		MaxBodyBytes: internalBodyLimit,
	}
}

func (s *Server) agentPaymentWorkspace(
	ctx context.Context,
	in *agentReadRequest[paymentEvidenceArguments],
) (*agentReadResponse[paymentWorkspaceView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	w, err := s.store.CurrentPaymentReviewWorkspace(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.EvidenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentReadResult(in.Body.OperationID, paymentWorkspaceView{}), nil
	}
	if err != nil {
		return nil, paymentError(err)
	}
	return agentReadResult(in.Body.OperationID, paymentWorkspaceView{Workspace: &w}), nil
}

func (s *Server) agentPaymentWorkspaceOperation(
	ctx context.Context,
	in *agentReadRequest[paymentWorkspaceOperationArguments],
) (*agentReadResponse[paymentWorkspaceOperationView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.PaymentReviewWorkspaceOperation(ctx, scope, in.Body.Arguments.OperationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentReadResult(in.Body.OperationID, paymentWorkspaceOperationView{}), nil
	}
	if err != nil {
		return nil, paymentError(err)
	}
	return agentReadResult(in.Body.OperationID, paymentWorkspaceOperationView{Receipt: &receipt}), nil
}

func (s *Server) agentSavePaymentReview(
	ctx context.Context,
	in *paymentWriteRequest[db.PaymentReviewDraftChange],
) (*paymentWriteResponse[db.PaymentReviewWorkspace], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation != nil &&
		(body.Confirmation.Mode != confirmationNone || body.Confirmation.Preconditions != nil) {
		return nil, huma.Error400BadRequest("matching operation key and no-confirmation mode required")
	}
	result, err := s.store.PreparePaymentReviewWorkspace(ctx, scope, body.OperationID, body.Arguments)
	return paymentWriteResult(body.OperationID, result, err)
}

func (s *Server) agentCancelPaymentReview(
	ctx context.Context,
	in *paymentWriteRequest[db.PaymentReviewWorkspaceChange],
) (*paymentWriteResponse[db.PaymentReviewWorkspace], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation != nil &&
		(body.Confirmation.Mode != confirmationNone || body.Confirmation.Preconditions != nil) {
		return nil, huma.Error400BadRequest("matching operation key and no-confirmation mode required")
	}
	result, err := s.store.CancelPaymentReviewWorkspace(ctx, scope, body.OperationID, body.Arguments)
	return paymentWriteResult(body.OperationID, result, err)
}

func (s *Server) preparePaymentWorkspaceCommit(
	ctx context.Context,
	in *agentReadRequest[db.PaymentReviewCommitChange],
) (*paymentWorkspacePreparationResponse, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prepared, err := s.store.PreparePaymentReviewCommit(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, paymentError(err)
	}
	out := &paymentWorkspacePreparationResponse{}
	out.Body.Status = agentPrepared
	out.Body.OperationID = in.Body.OperationID
	out.Body.Arguments = prepared.Arguments
	out.Body.Preconditions.ProposalID = prepared.ID
	out.Body.Description = prepared.Description
	out.Body.ExpiresAt = prepared.ExpiresAt
	return out, nil
}

func (s *Server) commitPaymentWorkspace(
	ctx context.Context,
	in *paymentWriteRequest[db.PaymentReviewCommitChange],
) (*paymentWriteResponse[db.PaymentWorkspaceReviewResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
		body.Confirmation.Mode != assignmentConfirmationMode || body.Confirmation.Preconditions == nil ||
		body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
	}
	result, err := s.store.CommitPaymentReviewWorkspace(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	return paymentWriteResult(body.OperationID, result, err)
}

func (s *Server) paymentWorkspaceReceipt(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (db.PaymentReviewWorkspaceReceipt, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return db.PaymentReviewWorkspaceReceipt{}, err
	}
	return s.store.PaymentReviewWorkspaceOperation(ctx, scope, in.Body.OperationID)
}

func (s *Server) paymentWorkspaceSaveStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.PaymentReviewWorkspace], error) {
	receipt, err := s.paymentWorkspaceReceipt(ctx, in)
	if receipt.Kind != "prepare" {
		receipt.Workspace = nil
	}
	return paymentStatusResult(in.Body.OperationID, receipt.Workspace, err)
}

func (s *Server) paymentWorkspaceCancelStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.PaymentReviewWorkspace], error) {
	receipt, err := s.paymentWorkspaceReceipt(ctx, in)
	if receipt.Kind != "cancel" {
		receipt.Workspace = nil
	}
	return paymentStatusResult(in.Body.OperationID, receipt.Workspace, err)
}

func (s *Server) paymentWorkspaceCommitStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.PaymentWorkspaceReviewResult], error) {
	receipt, err := s.paymentWorkspaceReceipt(ctx, in)
	if err != nil || receipt.Kind != "commit" {
		return paymentStatusResult[db.PaymentWorkspaceReviewResult](in.Body.OperationID, nil, err)
	}
	if receipt.Completed {
		return paymentStatusResult(in.Body.OperationID, receipt.Review, nil)
	}
	out := &paymentWriteResponse[db.PaymentWorkspaceReviewResult]{}
	out.Body.OperationID = in.Body.OperationID
	out.Body.Status = agentPending
	if receipt.UnavailableReason != "" {
		out.Body.Status = "cancelled"
		out.Body.Error = &agentRegistrationError{
			Code:           receipt.UnavailableReason,
			Message:        "prepared payment call cannot execute",
			Classification: agentTerminalRejection,
		}
	}
	return out, nil
}
