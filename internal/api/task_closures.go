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

const (
	taskClosurePath            = "/internal/agent/administration/close_task"
	taskClosureReasonMaxLength = 512
)

type taskClosurePreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.TaskClosureArguments `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

func (s *Server) registerTaskClosures(api huma.API) {
	registerAgentPost(
		api,
		"agent-prepare-task-closure",
		taskClosurePath+"/prepare",
		internalBodyLimit,
		s.prepareTaskClosure,
	)
	registerAgentPost(api, "agent-confirm-task-closure", taskClosurePath, internalBodyLimit, s.confirmTaskClosure)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-task-closure-"+suffix,
			taskClosurePath+"/"+suffix,
			internalBodyLimit,
			s.taskClosureStatus,
		)
	}
}

func taskClosureError(err error) error {
	switch {
	case errors.Is(err, db.ErrAdminDenied):
		return huma.Error403Forbidden("active global administrator required")
	case errors.Is(err, db.ErrTaskClosureInvalid), errors.Is(err, db.ErrAdminInvalid):
		return huma.Error400BadRequest("invalid task closure")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("task or proposal not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("task or exact proposal changed, is leased or expired")
	default:
		return huma.Error503ServiceUnavailable("task closure unavailable")
	}
}

func (s *Server) prepareTaskClosure(
	ctx context.Context, in *agentReadRequest[db.TaskClosureArguments],
) (*taskClosurePreparationResponse, error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PrepareTaskClosure(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, taskClosureError(err)
	}
	out := &taskClosurePreparationResponse{}
	out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
	out.Body.Arguments, out.Body.Description, out.Body.ExpiresAt = prep.Arguments, prep.Description, prep.ExpiresAt
	out.Body.Preconditions.ProposalID = prep.ID
	return out, nil
}

func (s *Server) confirmTaskClosure(
	ctx context.Context, in *paymentWriteRequest[db.TaskClosureArguments],
) (*paymentWriteResponse[db.TaskClosureResult], error) {
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
	result, err := s.store.ConfirmTaskClosure(ctx, scope, body.OperationID, body.Arguments,
		body.Confirmation.Preconditions.ProposalID)
	out := &paymentWriteResponse[db.TaskClosureResult]{}
	out.Body.OperationID = body.OperationID
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrTaskClosureInvalid) ||
		errors.Is(err, db.ErrAdminInvalid) || errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code: "task_closure_rejected", Message: "Task or exact proposal changed, is missing, leased or expired",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, taskClosureError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) taskClosureStatus(
	ctx context.Context, in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.TaskClosureResult], error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.TaskClosureReceipt(ctx, scope, in.Body.OperationID)
	out := &paymentWriteResponse[db.TaskClosureResult]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, taskClosureError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, receipt
	return out, nil
}

func taskClosureMethod() map[string]any {
	method := administrativeRead(
		"close_task",
		"Close an uncertain local task only after inspecting its diagnostics and external result. Requires current global administrator authority and exact approval of task ID, version and reason. Stops local processing and blocks pending/retryable deliveries while preserving uncertainty evidence. Does not prove an external outcome, retry delivery, cancel a remote Loom run or change money. Reconcile the same operation after a lost response; never infer success from not_found or replace it blindly.",
		map[string]any{
			schemaTaskID:          map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			schemaReason: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: taskClosureReasonMaxLength,
			},
		},
		[]string{schemaTaskID, schemaExpectedVersion, schemaReason},
		[]string{schemaTaskID, "resolution"},
	)
	method[manifestEffect] = effectWrite
	method[manifestIdempotency] = true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = taskClosurePath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode: assignmentConfirmationMode, manifestPreparePath: taskClosurePath + "/prepare",
	}
	return method
}
