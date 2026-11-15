package db

import (
	"context"

	"github.com/google/uuid"
)

// CommunicationByOperation resolves only this author's preparation in the
// deployment. The normal read rechecks the active owner and returns its status.
func (s *Store) CommunicationByOperation(
	ctx context.Context,
	scope PaymentScope,
	operation uuid.UUID,
) (Communication, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM shuttle_communications WHERE bot_id=$1
 AND organization_id=$2 AND author_id=$3 AND operation_id=$4`,
		scope.BotID, scope.Organization, scope.UserID, operation).Scan(&id)
	if err != nil {
		return Communication{}, err
	}
	return s.Communication(ctx, scope, id)
}
