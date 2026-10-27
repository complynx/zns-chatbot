package db

import (
	"context"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// PairAssignmentProposalByOperation resolves the actor-owned operation or adoption
// alias, then reads original terms and current identities under current authority.
func (s *Store) PairAssignmentProposalByOperation(ctx context.Context, scope PaymentScope,
	operation string,
) (PairAssignmentProposalView, error) {
	if !paymentScopeValid(scope, operation) {
		return PairAssignmentProposalView{}, registrations.ErrInvalid
	}
	row, err := s.q.GetPairPreparation(ctx, sqlc.GetPairPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: operation,
	})
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	return s.PairAssignmentProposal(ctx, scope, row.ProposalID)
}
