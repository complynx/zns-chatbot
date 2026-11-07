package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type pairCandidateListResult struct {
	Candidates []db.PairAssignmentCandidate `json:"candidates"`
	Next       *uuid.UUID                   `json:"next,omitempty"`
}

func (s *Server) registerPairPrerequisites(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "agent-pair-events", Method: http.MethodPost,
		Path: "/internal/agent/pairs/events", MaxBodyBytes: internalBodyLimit}, s.pairAssignmentEvents)
	huma.Register(api, huma.Operation{OperationID: "agent-pair-candidates", Method: http.MethodPost,
		Path: "/internal/agent/pairs/candidates", MaxBodyBytes: internalBodyLimit}, s.pairAssignmentCandidates)
}

func (s *Server) pairAssignmentEvents(
	ctx context.Context,
	in *agentReadRequest[agentEventListArguments],
) (*agentReadResponse[agentEventListResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.PairAssignmentEvents(ctx, scope, args.After, args.Limit)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	result := agentEventListResult{Events: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) pairAssignmentCandidates(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationListArguments],
) (*agentReadResponse[pairCandidateListResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.PairAssignmentCandidates(ctx, scope, args.EventID, args.After, args.Limit)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	result := pairCandidateListResult{Candidates: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].Pair.ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
