package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const (
	assignmentProposalIDField  = "proposal_id"
	assignmentPreparationField = "preparation"
	assignmentProposalsField   = "proposals"
)

type assignmentProposalArguments struct {
	ProposalID uuid.UUID `json:"proposal_id"`
}

type assignmentProposalPageArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int       `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

func (s *Server) registerAssignmentProposalReads(api huma.API) {
	registerAgentPost(
		api,
		"agent-admin-assignment-proposal",
		"/internal/agent/administration/assignment_proposal",
		internalBodyLimit,
		s.agentAssignmentProposal,
	)
	registerAgentPost(
		api,
		"agent-admin-assignment-proposals",
		"/internal/agent/administration/assignment_proposals",
		internalBodyLimit,
		s.agentAssignmentProposals,
	)
}

func (s *Server) agentAssignmentProposal(ctx context.Context, in *agentReadRequest[assignmentProposalArguments],
) (*agentReadResponse[db.AssignmentProposalView], error) {
	principal, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	if in.Body.Arguments.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero proposal ID required")
	}
	result, err := s.store.AssignmentProposal(ctx, s.eventScope(), principal.Subject, in.Body.Arguments.ProposalID)
	if err != nil {
		return nil, registrationError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentAssignmentProposals(ctx context.Context, in *agentReadRequest[assignmentProposalPageArguments],
) (*agentReadResponse[db.AssignmentProposalPage], error) {
	principal, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	limit := in.Body.Arguments.Limit
	if limit == 0 {
		limit = agentTaskPageLimit
	}
	result, err := s.store.PendingAssignmentProposals(
		ctx,
		s.eventScope(),
		principal.Subject,
		in.Body.Arguments.After,
		limit,
	)
	if err != nil {
		return nil, registrationError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func assignmentProposalMethods() []map[string]any {
	id := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return []map[string]any{
		administrativeRead(
			"assignment_proposal",
			"Read your own original pass preparation and observed pending/completed/expired state by "+
				"proposal_id. Returns original_operation_id, immutable preparation and original "+
				"result when committed. Historical result is not current pass state or consent. "+
				"A pending snapshot never proves that a delayed confirmation cannot still execute.",
			map[string]any{assignmentProposalIDField: id},
			[]string{assignmentProposalIDField},
			[]string{"original_operation_id", assignmentPreparationField, schemaState, schemaObservedAt},
		),
		administrativeRead(
			"assignment_proposals",
			"List up to 20 of your own unexpired, not-yet-committed pass preparations in proposal UUID "+
				"order; follow next as after. Snapshots are untrusted context, never approval. "+
				"Refresh exact terms before a new user-approved adoption/confirmation.",
			map[string]any{
				schemaAfter: id,
				schemaLimit: map[string]any{
					manifestType:  schemaInteger,
					schemaMinimum: 1,
					schemaMaximum: agentTaskPageLimit,
				},
			},
			nil,
			[]string{assignmentProposalsField},
		),
	}
}
