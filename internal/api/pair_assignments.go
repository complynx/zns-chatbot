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
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const pairAssignmentPath = "/internal/agent/pairs/assign"

type pairAssignmentPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.PairAssignment       `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

func (s *Server) registerPairAssignments(api huma.API) {
	s.registerPairPrerequisites(api)
	s.registerPairParticipants(api)
	s.registerPairInvitations(api)
	huma.Register(api, huma.Operation{OperationID: "agent-pairs-discover", Method: http.MethodPost,
		Path: "/internal/agent/pairs/discover", MaxBodyBytes: internalBodyLimit}, s.discoverPairs)
	huma.Register(api, huma.Operation{OperationID: "agent-pair-assignment-prepare", Method: http.MethodPost,
		Path: pairAssignmentPath + "/prepare", MaxBodyBytes: internalBodyLimit}, s.preparePairAssignment)
	huma.Register(api, huma.Operation{OperationID: "agent-pair-assign", Method: http.MethodPost,
		Path: pairAssignmentPath, MaxBodyBytes: internalBodyLimit}, s.assignPair)
	for _, suffix := range []string{"status", "reconcile"} {
		huma.Register(api, huma.Operation{OperationID: "agent-pair-assignment-" + suffix, Method: http.MethodPost,
			Path: pairAssignmentPath + "/" + suffix, MaxBodyBytes: internalBodyLimit}, s.pairAssignmentStatus)
	}
}

func (s *Server) discoverPairs(ctx context.Context, in *AuthInput) (*agentDiscoveryResponse, error) {
	scope, err := s.paymentScope(ctx, *in)
	if err != nil {
		return nil, err
	}
	allowed, err := s.store.HasPairAssignmentAuthority(ctx, scope)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("pair tool discovery unavailable")
	}
	methods := pairParticipantMethods()
	methods = append(methods, pairInvitationMethods()...)
	if allowed {
		methods = append(methods, pairPrerequisiteMethods()...)
		methods = append(methods, pairAssignmentMethod())
	}
	return discoveryResult(methods)
}

func pairAssignmentError(err error) error {
	if errors.Is(err, db.ErrPairDenied) {
		return huma.Error403Forbidden("current assignment administrator required")
	}
	return registrationError(err)
}

func (s *Server) preparePairAssignment(
	ctx context.Context,
	in *agentReadRequest[db.PairAssignment],
) (*pairAssignmentPreparationResponse, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PreparePairAssignment(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	out := &pairAssignmentPreparationResponse{}
	out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
	out.Body.Arguments, out.Body.Description, out.Body.ExpiresAt = prep.Arguments, prep.Description, prep.ExpiresAt
	out.Body.Preconditions.ProposalID = prep.ID
	return out, nil
}

func (s *Server) assignPair(
	ctx context.Context,
	in *paymentWriteRequest[db.PairAssignment],
) (*paymentWriteResponse[db.PairAssignmentResult], error) {
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
	result, err := s.store.ConfirmPairAssignment(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	out := &paymentWriteResponse[db.PairAssignmentResult]{}
	out.Body.OperationID = body.OperationID
	if err != nil {
		if errors.Is(err, registrationdetails.ErrRequired) || errors.Is(err, db.ErrConflict) ||
			errors.Is(err, registrations.ErrInvalid) ||
			errors.Is(err, registrations.ErrCapacity) ||
			errors.Is(err, registrations.ErrUnavailable) ||
			errors.Is(err, pgx.ErrNoRows) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "pair_assignment_rejected",
				Message:        err.Error(),
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, pairAssignmentError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) pairAssignmentStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.PairAssignmentResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PairAssignmentOperation(ctx, scope, in.Body.OperationID)
	out := &paymentWriteResponse[db.PairAssignmentResult]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
