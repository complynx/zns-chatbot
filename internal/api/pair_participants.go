package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const pairDissolutionPath = "/internal/agent/pairs/dissolve"

const routeReconcile = "reconcile"

const routeStatus = "status"

type ownedPairArguments struct {
	EventID uuid.UUID `json:"event_id,omitempty"`
	After   uuid.UUID `json:"after,omitempty"`
	Limit   int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type ownedPairListResult struct {
	Pairs []db.OwnedPair `json:"pairs"`
	Next  *uuid.UUID     `json:"next,omitempty"`
}

func (s *Server) registerPairParticipants(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "agent-owned-pairs", Method: http.MethodPost,
		Path: "/internal/agent/pairs/my", MaxBodyBytes: internalBodyLimit}, s.ownedPairs)
	huma.Register(api, huma.Operation{OperationID: "agent-pair-dissolve", Method: http.MethodPost,
		Path: pairDissolutionPath, MaxBodyBytes: internalBodyLimit}, s.dissolvePair)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		huma.Register(api, huma.Operation{OperationID: "agent-pair-dissolution-" + suffix, Method: http.MethodPost,
			Path: pairDissolutionPath + "/" + suffix, MaxBodyBytes: internalBodyLimit}, s.pairDissolutionStatus)
	}
}

func pairParticipantError(err error) error {
	if errors.Is(err, db.ErrPairDenied) {
		return huma.Error403Forbidden("active pair participant required")
	}
	return registrationError(err)
}

func (s *Server) ownedPairs(
	ctx context.Context,
	in *agentReadRequest[ownedPairArguments],
) (*agentReadResponse[ownedPairListResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.ownedPairPage(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, pairParticipantError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) ownedPairPage(
	ctx context.Context,
	scope db.PaymentScope,
	args ownedPairArguments,
) (ownedPairListResult, error) {
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.OwnedPairs(ctx, scope, args.EventID, args.After, args.Limit)
	if err != nil {
		return ownedPairListResult{}, err
	}
	result := ownedPairListResult{Pairs: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].Pair.ID
	}
	return result, nil
}

func (s *Server) dissolvePair(
	ctx context.Context,
	in *paymentWriteRequest[db.PairDissolution],
) (*paymentWriteResponse[registrations.Pair], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID ||
		body.Confirmation != nil && body.Confirmation.Mode != confirmationNone {
		return nil, huma.Error400BadRequest("matching operation key and participant action required")
	}
	result, err := s.store.DissolvePair(ctx, scope, body.OperationID, body.Arguments)
	out := &paymentWriteResponse[registrations.Pair]{}
	out.Body.OperationID = body.OperationID
	if err != nil {
		if errors.Is(err, db.ErrConflict) || errors.Is(err, registrations.ErrInvalid) || errors.Is(err, pgx.ErrNoRows) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "pair_dissolution_rejected",
				Message:        err.Error(),
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, pairParticipantError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) pairDissolutionStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[registrations.Pair], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PairDissolutionOperation(ctx, scope, in.Body.OperationID)
	out := &paymentWriteResponse[registrations.Pair]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, pairParticipantError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
