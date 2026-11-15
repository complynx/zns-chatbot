package db

import (
	"context"
)

// CheckCommunicationRecipients checks stored relationships without requiring
// recipient IAM. A verified global-admin target is a separate fallback grant.
func (s *Store) CheckCommunicationRecipients(ctx context.Context, scope PaymentScope, a CommunicationArguments) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockCommunicationActor(ctx, tx, scope, a.EventID); err != nil {
		return err
	}
	return communicationRecipientsAccess(
		ctx,
		tx,
		scope,
		a.EventID,
		a.Recipients,
		a.Category == communicationPersonal,
		false,
	)
}
