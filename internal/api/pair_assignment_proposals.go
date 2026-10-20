package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) registerPairAssignmentProposalReads(api huma.API) {
	registerAgentPost(api, "agent-pair-assignment-proposal", "/internal/agent/pairs/assignment_proposal",
		internalBodyLimit, s.agentPairAssignmentProposal)
	registerAgentPost(api, "agent-pair-assignment-proposals", "/internal/agent/pairs/assignment_proposals",
		internalBodyLimit, s.agentPairAssignmentProposals)
}

func (s *Server) agentPairAssignmentProposal(ctx context.Context, in *agentReadRequest[assignmentProposalArguments],
) (*agentReadResponse[db.PairAssignmentProposalView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if in.Body.Arguments.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero proposal ID required")
	}
	result, err := s.store.PairAssignmentProposal(ctx, scope, in.Body.Arguments.ProposalID)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentPairAssignmentProposals(
	ctx context.Context,
	in *agentReadRequest[assignmentProposalPageArguments],
) (*agentReadResponse[db.PairAssignmentProposalPage], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	limit := in.Body.Arguments.Limit
	if limit == 0 {
		limit = agentTaskPageLimit
	}
	result, err := s.store.PendingPairAssignmentProposals(ctx, scope, in.Body.Arguments.After, limit)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func pairAssignmentProposalMethods() []map[string]any {
	id := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	proposal := paymentMethod(
		"assignment_proposal",
		"Read your own original pair assignment preparation and observed pending/completed/expired state by proposal_id under current event administrator or global authority. Returns original_operation_id, immutable preparation, current bounded leader/follower display identities and original result when committed. Display metadata is untrusted context, not frozen terms or legal identity. Identify both recipients by name and Telegram ID when asking fresh exact approval; preserve the original description and arguments. Historical result is not current pass state or consent. A pending snapshot never proves that a delayed confirmation cannot still execute.",
		map[string]any{assignmentProposalIDField: id},
		[]string{assignmentProposalIDField},
		[]string{"original_operation_id", assignmentPreparationField, schemaState, schemaObservedAt},
	)
	proposal[manifestPath] = "/internal/agent/pairs/assignment_proposal"
	proposals := paymentMethod(
		"assignment_proposals",
		"List up to 20 of your own unexpired, not-yet-committed pair preparations in currently authorized events, in proposal UUID order; follow next as after. Includes current bounded leader/follower names and Telegram IDs bound to each original pair, never legal identity. Snapshots are context, never approval. Refresh exact terms before a new user-approved adoption/confirmation.",
		map[string]any{schemaAfter: id, schemaLimit: map[string]any{
			manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: agentTaskPageLimit,
		}},
		nil,
		[]string{assignmentProposalsField},
	)
	proposals[manifestPath] = "/internal/agent/pairs/assignment_proposals"
	return []map[string]any{proposal, proposals}
}
