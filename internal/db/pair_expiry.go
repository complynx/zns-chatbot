package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const pairExpiryPageSize = 100

// ExpirePairInvitations releases durable holds without depending on IAM or Loom.
// The returned cursor advances only after a successful page; repeated pages are safe.
func (s *Store) ExpirePairInvitations(ctx context.Context, scope EventScope, after uuid.UUID) (uuid.UUID, error) {
	rows, err := s.q.ExpiredPairInvitations(ctx, sqlc.ExpiredPairInvitationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, AfterID: after, PageLimit: pairExpiryPageSize,
	})
	if err != nil {
		return after, err
	}
	for _, row := range rows {
		if err = s.expirePairInvitation(ctx, scope, row.EventID, row.ID); err != nil {
			return after, err
		}
	}
	if len(rows) < pairExpiryPageSize {
		return uuid.Nil, nil
	}
	return rows[len(rows)-1].ID, nil
}

func (s *Store) expirePairInvitation(ctx context.Context, scope EventScope, eventID, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err = q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
		ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization,
	}); err != nil {
		return err
	}
	row, err := q.GetPairInvitation(ctx, sqlc.GetPairInvitationParams{ID: id, EventID: eventID})
	if err != nil {
		return err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if row.State != pairPending || now.Time.Before(row.ExpiresAt.Time) {
		return nil
	}
	if _, err = q.DecidePairInvitation(ctx, sqlc.DecidePairInvitationParams{
		ID: row.ID, Version: row.Version, State: pairExpired, DecidedAt: now,
	}); err != nil {
		return err
	}
	if _, err = q.AdvancePairRegistration(ctx, sqlc.AdvancePairRegistrationParams{
		ID: row.InviterRegistrationID, EventID: eventID,
	}); err != nil {
		return err
	}
	if err = enqueuePairInvitationNotice(ctx, q, row.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
