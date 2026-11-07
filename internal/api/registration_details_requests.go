package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type detailsRequestArguments struct {
	RequestID uuid.UUID `json:"request_id"`
}
type detailsRequestsArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty" minimum:"1" maximum:"20" default:"20"`
}
type detailsRequestsPage struct {
	Requests []db.DetailsRequestStatus `json:"requests"`
	Next     *uuid.UUID                `json:"next,omitempty"`
}
type detailsRequestResponse struct {
	Body struct {
		Status      string                   `json:"status"`
		OperationID string                   `json:"operation_id"`
		Result      *db.DetailsRequestStatus `json:"result,omitempty"`
		Error       *agentRegistrationError  `json:"error,omitempty"`
	}
}

func (s *Server) registerDetailsRequests(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-details-readiness",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "readiness",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentDetailsReadiness,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-details-request-begin",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "begin_request",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentBeginDetailsRequest,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-details-request-read",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "request",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentDetailsRequest,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-details-request-operation",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "request_operation",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentDetailsRequestOperation,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-details-requests-list",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "requests",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentDetailsRequests,
	)
}

func (s *Server) agentDetailsReadiness(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.DetailsReadiness], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if in.Body.Arguments.EventID == uuid.Nil {
		return nil, huma.Error400BadRequest("event ID must be a nonzero UUID")
	}
	result, err := s.store.DetailsReadiness(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, registrationError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentBeginDetailsRequest(
	ctx context.Context,
	in *profileWriteRequest[db.DetailsRequestStart],
) (*detailsRequestResponse, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
	}
	result, err := s.store.BeginDetailsRequest(ctx, scope, operation, in.Body.Arguments)
	return detailsRequestResult(in.Body.OperationID, result, err)
}

func (s *Server) agentDetailsRequest(
	ctx context.Context,
	in *agentReadRequest[detailsRequestArguments],
) (*agentReadResponse[db.DetailsRequestStatus], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if in.Body.Arguments.RequestID == uuid.Nil {
		return nil, huma.Error400BadRequest("request ID must be a nonzero UUID")
	}
	result, err := s.store.DetailsRequest(ctx, scope, in.Body.Arguments.RequestID)
	if err != nil {
		return nil, registrationError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentDetailsRequestOperation(
	ctx context.Context,
	in *profileOperationRequest,
) (*detailsRequestResponse, error) {
	scope, operation, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.DetailsRequestOperation(ctx, scope, operation)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &detailsRequestResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return detailsRequestResult(in.Body.OperationID, result, err)
}

func (s *Server) agentDetailsRequests(
	ctx context.Context,
	in *agentReadRequest[detailsRequestsArguments],
) (*agentReadResponse[detailsRequestsPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	limit := in.Body.Arguments.Limit
	if limit == 0 {
		limit = detailsPageLimit
	}
	requests, err := s.store.ListDetailsRequests(ctx, scope, in.Body.Arguments.After, limit)
	if err != nil {
		return nil, registrationError(err)
	}
	page := detailsRequestsPage{Requests: requests}
	if len(requests) == int(limit) {
		next := requests[len(requests)-1].Request.ID
		page.Next = &next
	}
	return agentReadResult(in.Body.OperationID, page), nil
}

func detailsRequestResult(
	operation string,
	result db.DetailsRequestStatus,
	err error,
) (*detailsRequestResponse, error) {
	out := &detailsRequestResponse{}
	out.Body.OperationID = operation
	if err != nil {
		if errors.Is(err, registrationdetails.ErrInvalid) || errors.Is(err, db.ErrConflict) ||
			errors.Is(err, pgx.ErrNoRows) ||
			errors.Is(err, registrations.ErrUnavailable) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "details_request_rejected",
				Message:        "Request changed, invalid or unavailable; read current event and readiness before a new operation.",
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, huma.Error503ServiceUnavailable("registration details request unavailable")
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
