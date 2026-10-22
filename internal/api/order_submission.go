package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type orderSubmitRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                   `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID             `json:"operation_id"`
		RunID           string                `json:"run_id,omitempty"`
		Arguments       db.OrderSubmission    `json:"arguments"`
		IdempotencyKey  uuid.UUID             `json:"idempotency_key"`
		Confirmation    authorityConfirmation `json:"confirmation"`
	}
}

type orderPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.OrderSubmission      `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

type orderDetailArguments struct {
	EventID uuid.UUID `json:"event_id"`
	OrderID uuid.UUID `json:"order_id"`
}

type orderOperationArguments struct {
	OperationID uuid.UUID             `json:"operation_id"`
	Kind        db.OrderOperationKind `json:"kind"         enum:"prepare,submit,cancel"`
}

// The business bindings are fixed when routes are registered; requests cannot
// select another order kind or bypass that module's store authorization.
func (s *orderAgentAPI) register(api huma.API, path, prefix string) {
	route := func(method string) huma.Operation {
		return huma.Operation{OperationID: prefix + method, Method: http.MethodPost,
			Path: path + method, MaxBodyBytes: internalBodyLimit}
	}
	huma.Register(api, route("submit/prepare"), s.agentPrepareOrder)
	huma.Register(api, route("submit"), s.agentSubmitOrder)
	huma.Register(api, route("submit/status"), s.agentOrderSubmissionStatus)
	huma.Register(api, route("cancel"), s.agentCancelOrder)
	huma.Register(api, route("cancel/status"), s.agentOrderCancellationStatus)
	huma.Register(api, route("get"), s.agentOwnOrder)
	huma.Register(api, route("list"), s.agentOwnOrders)
	huma.Register(api, route("operation"), s.agentOrderOperation)
}

func (s *orderAgentAPI) agentPrepareOrder(
	ctx context.Context,
	in *agentReadRequest[db.OrderSubmission],
) (*orderPreparationResponse, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	prepared, err := s.actions.prepare(ctx, scope, op, in.Body.Arguments, db.OrderSourceAgent)
	if err != nil {
		return nil, s.mapError(err)
	}
	language, err := s.orderPresentationLanguage(ctx, scope)
	if err != nil {
		return nil, s.mapError(err)
	}
	description := prepared.Description
	if description == "" {
		description, err = s.describe(prepared, language)
		if err != nil {
			return nil, s.mapError(err)
		}
	}
	out := &orderPreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = description
	return out, nil
}

func (s *orderAgentAPI) agentSubmitOrder(
	ctx context.Context,
	in *orderSubmitRequest,
) (*paymentWriteResponse[db.OrderSubmissionResult], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if in.Body.OperationID == uuid.Nil || in.Body.OperationID != in.Body.IdempotencyKey ||
		in.Body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching nonzero operation key and exact proposal confirmation required")
	}
	result, err := s.actions.submit(
		ctx,
		scope,
		in.Body.OperationID,
		in.Body.Arguments,
		in.Body.Confirmation.Preconditions.ProposalID,
		db.OrderSourceAgent,
	)
	return orderWriteResult(in.Body.OperationID.String(), result, err, s.mapError)
}

func (s *orderAgentAPI) agentCancelOrder(
	ctx context.Context,
	in *profileWriteRequest[db.OrderCancellation],
) (*paymentWriteResponse[db.SubmittedOrder], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
	}
	result, err := s.actions.cancel(ctx, scope, op, in.Body.Arguments, db.OrderSourceAgent)
	return orderWriteResult(in.Body.OperationID, result, err, s.mapError)
}

func (s *orderAgentAPI) agentOwnOrder(
	ctx context.Context,
	in *agentReadRequest[orderDetailArguments],
) (*agentReadResponse[db.SubmittedOrder], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.actions.get(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.OrderID)
	if err != nil {
		return nil, s.mapError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *orderAgentAPI) agentOwnOrders(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationListArguments],
) (*agentReadResponse[db.OrdersPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = 20
	}
	result, err := s.actions.list(ctx, scope, args.EventID, args.After, args.Limit)
	if err != nil {
		return nil, s.mapError(err)
	}
	result, err = boundedOrdersPage(result)
	if err != nil {
		return nil, s.mapError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func orderWriteResult[T any](
	operation string,
	result T,
	err error,
	mapError func(error) error,
) (*paymentWriteResponse[T], error) {
	out := &paymentWriteResponse[T]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := mapError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "order_operation_rejected",
			Message:        "order, draft, versions, proposal or current access rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *orderAgentAPI) orderOperationStatus(
	ctx context.Context,
	in *profileOperationRequest,
	kind db.OrderOperationKind,
) (*paymentWriteResponse[json.RawMessage], error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.actions.operation(ctx, scope, op, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[json.RawMessage]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return orderWriteResult(in.Body.OperationID, result.Result, err, s.mapError)
}

func (s *orderAgentAPI) agentOrderSubmissionStatus(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[json.RawMessage], error) {
	return s.orderOperationStatus(ctx, in, db.OrderSubmit)
}

func (s *orderAgentAPI) agentOrderCancellationStatus(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[json.RawMessage], error) {
	return s.orderOperationStatus(ctx, in, db.OrderCancel)
}

func (s *orderAgentAPI) agentOrderOperation(
	ctx context.Context,
	in *agentReadRequest[orderOperationArguments],
) (*paymentWriteResponse[db.OrderOperation], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.actions.operation(ctx, scope, in.Body.Arguments.OperationID, in.Body.Arguments.Kind)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.OrderOperation]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return orderWriteResult(in.Body.OperationID, result, err, s.mapError)
}
