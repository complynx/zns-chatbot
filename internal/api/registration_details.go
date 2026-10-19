package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

const registrationDetailsPath = "/internal/agent/registration_details/"

type legalNameArguments struct {
	ExpectedVersion int64  `json:"expected_version" minimum:"0"`
	LegalName       string `json:"legal_name"                   minLength:"1" maxLength:"256"`
}

type passportNumberArguments struct {
	ExpectedVersion int64  `json:"expected_version" minimum:"0"`
	PassportNumber  string `json:"passport_number"              minLength:"1" maxLength:"128"`
}

type registrationDetailsResponse struct {
	Body struct {
		Status      string                      `json:"status"`
		OperationID string                      `json:"operation_id"`
		Result      *registrationdetails.Status `json:"result,omitempty"`
		Error       *agentRegistrationError     `json:"error,omitempty"`
	}
}

func (s *Server) registerRegistrationDetails(api huma.API) {
	if s.options.RegistrationDetails == nil {
		return
	}
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-registration-details-get",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "get",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentRegistrationDetails,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-registration-details-operation",
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + "operation",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentRegistrationDetailsOperation,
	)
	s.registerDetailsRequests(api)
	registerDetailsMutation(s, api, "set_legal_name", func(a legalNameArguments) registrationdetails.Mutation {
		return registrationdetails.Mutation{
			Field:           registrationdetails.LegalName,
			ExpectedVersion: a.ExpectedVersion,
			Value:           a.LegalName,
		}
	})
	registerDetailsMutation(
		s,
		api,
		"set_passport_number",
		func(a passportNumberArguments) registrationdetails.Mutation {
			return registrationdetails.Mutation{
				Field:           registrationdetails.PassportNumber,
				ExpectedVersion: a.ExpectedVersion,
				Value:           a.PassportNumber,
			}
		},
	)
}

func registerDetailsMutation[T any](
	s *Server,
	api huma.API,
	method string,
	mutation func(T) registrationdetails.Mutation,
) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-registration-details-" + method,
			Method:       http.MethodPost,
			Path:         registrationDetailsPath + method,
			MaxBodyBytes: internalBodyLimit,
		},
		func(ctx context.Context, in *profileWriteRequest[T]) (*registrationDetailsResponse, error) {
			scope, err := s.profileScope(ctx, in.AuthInput)
			if err != nil {
				return nil, err
			}
			operation, err := uuid.Parse(in.Body.OperationID)
			if err != nil || operation == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
				return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
			}
			change := mutation(in.Body.Arguments)
			change.OperationID = operation
			result, err := s.store.MutateRegistrationDetails(ctx, scope, change, s.options.RegistrationDetails)
			return registrationDetailsResult(in.Body.OperationID, result, err)
		},
	)
}

func (s *Server) agentRegistrationDetails(
	ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[registrationdetails.Status], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.RegistrationDetailsStatus(ctx, scope)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("registration details unavailable")
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentRegistrationDetailsOperation(
	ctx context.Context,
	in *profileOperationRequest,
) (*registrationDetailsResponse, error) {
	scope, operation, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.RegistrationDetailsOperation(ctx, scope, operation)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &registrationDetailsResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return registrationDetailsResult(in.Body.OperationID, result, err)
}

func registrationDetailsResult(
	operation string,
	result registrationdetails.Status,
	err error,
) (*registrationDetailsResponse, error) {
	out := &registrationDetailsResponse{}
	out.Body.OperationID = operation
	if err != nil {
		if errors.Is(err, registrationdetails.ErrInvalid) || errors.Is(err, db.ErrConflict) ||
			errors.Is(err, pgx.ErrNoRows) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "registration_details_rejected",
				Message:        "Registration details changed, invalid or unavailable; read current status before a new request.",
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, huma.Error503ServiceUnavailable("registration details unavailable")
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

// Framework validation can echo submitted values before the owner handler runs.
func registrationDetailsError(ctx huma.Context, _ string, value any) (any, error) {
	failure, ok := value.(*huma.ErrorModel)
	if !ok || !strings.HasPrefix(ctx.Operation().Path, registrationDetailsPath) {
		return value, nil
	}
	return &huma.ErrorModel{
		Status: failure.Status,
		Title:  http.StatusText(failure.Status),
		Detail: "Registration details request rejected or unavailable.",
	}, nil
}
