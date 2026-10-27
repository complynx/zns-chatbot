package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

const userRevocationPath = "/internal/agent/administration/revoke_user"

type userRevocationPreparationResponse struct {
	Body struct {
		Status        string                     `json:"status"`
		OperationID   string                     `json:"operation_id"`
		Arguments     db.UserRevocationArguments `json:"arguments"`
		Preconditions assignmentPreconditions    `json:"preconditions"`
		Description   string                     `json:"description"`
		ExpiresAt     time.Time                  `json:"expires_at"`
	}
}

type userRevocationDiagnosticArguments struct {
	ProposalID uuid.UUID `json:"proposal_id"`
}

func (s *Server) registerUserRevocations(api huma.API) {
	registerAgentPost(
		api,
		"agent-prepare-user-revocation",
		userRevocationPath+"/prepare",
		internalBodyLimit,
		s.prepareUserRevocation,
	)
	registerAgentPost(
		api,
		"agent-confirm-user-revocation",
		userRevocationPath,
		internalBodyLimit,
		s.confirmUserRevocation,
	)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-user-revocation-"+suffix,
			userRevocationPath+"/"+suffix,
			internalBodyLimit,
			s.userRevocationStatus,
		)
	}
	registerAgentPost(
		api,
		"agent-user-revocation-target",
		"/internal/agent/administration/user",
		internalBodyLimit,
		s.userRevocationTarget,
	)
	registerAgentPost(
		api,
		"agent-user-revocation-receipt",
		"/internal/agent/administration/user_revocation_receipt",
		internalBodyLimit,
		s.userRevocationDiagnostic,
	)
}

func userRevocationError(err error) error {
	switch {
	case errors.Is(err, db.ErrAdminDenied):
		return huma.Error403Forbidden("active global administrator required")
	case errors.Is(err, db.ErrAdminInvalid), errors.Is(err, db.ErrUserRevocationInvalid):
		return huma.Error400BadRequest("invalid local user revocation")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("user or revocation proposal not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("user or exact proposal changed or expired")
	default:
		return huma.Error503ServiceUnavailable("revocation uncertain or unavailable; reconcile the same operation")
	}
}

func (s *Server) prepareUserRevocation(
	ctx context.Context,
	in *agentReadRequest[db.UserRevocationArguments],
) (*userRevocationPreparationResponse, error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PrepareUserRevocation(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, userRevocationError(err)
	}
	out := &userRevocationPreparationResponse{}
	out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
	out.Body.Arguments, out.Body.Description, out.Body.ExpiresAt = prep.Arguments, prep.Description, prep.ExpiresAt
	out.Body.Preconditions.ProposalID = prep.ID
	return out, nil
}

// Invalidate trusted receipt metadata first, including an ambiguous commit. A
// replay may outlive identity rebinding, so also invalidate its current scoped subject.
func (s *Server) invalidateRevocation(ctx context.Context, result db.UserRevocationResult) error {
	if result.UserID == uuid.Nil {
		return nil
	}
	if result.Subject != "" {
		s.iam.Invalidate(result.Subject)
	}
	current, err := s.store.RevocationTarget(ctx, s.options.BotID, s.options.OrganizationID, result.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.ExpectedSubject != "" && current.ExpectedSubject != result.Subject {
		s.iam.Invalidate(current.ExpectedSubject)
	}
	return nil
}

func (s *Server) confirmUserRevocation(
	ctx context.Context,
	in *paymentWriteRequest[db.UserRevocationArguments],
) (*paymentWriteResponse[db.UserRevocationResult], error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
		body.Confirmation.Mode != assignmentConfirmationMode || body.Confirmation.Preconditions == nil ||
		body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
	}
	result, err := s.store.ConfirmUserRevocation(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	if syncErr := s.invalidateRevocation(ctx, result); syncErr != nil {
		return nil, userRevocationError(syncErr)
	}
	out := &paymentWriteResponse[db.UserRevocationResult]{}
	out.Body.OperationID = body.OperationID
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrUserRevocationInvalid) || errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "user_revocation_rejected",
			Message:        "User or exact proposal changed, is missing or expired",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, userRevocationError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) userRevocationStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.UserRevocationResult], error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.UserRevocationReceipt(ctx, scope, in.Body.OperationID)
	out := &paymentWriteResponse[db.UserRevocationResult]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, userRevocationError(err)
	}
	if err = s.invalidateRevocation(ctx, *receipt); err != nil {
		return nil, userRevocationError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, receipt
	return out, nil
}

func (s *Server) userRevocationTarget(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.UserRevocationArguments], error) {
	if _, err := s.adminScope(ctx, in.AuthInput); err != nil {
		return nil, err
	}
	result, err := s.store.RevocationTarget(ctx, s.options.BotID, s.options.OrganizationID, in.Body.Arguments.ID)
	if err != nil {
		return nil, userRevocationError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) userRevocationDiagnostic(
	ctx context.Context,
	in *agentReadRequest[userRevocationDiagnosticArguments],
) (*agentReadResponse[db.UserRevocationDiagnostic], error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.UserRevocationDiagnostic(ctx, scope, in.Body.Arguments.ProposalID)
	if err != nil {
		return nil, userRevocationError(err)
	}
	if result.Result != nil {
		if err = s.invalidateRevocation(ctx, *result.Result); err != nil {
			return nil, userRevocationError(err)
		}
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
