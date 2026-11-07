package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// PairInvitationNotification is a durable delivery intent, not proof of delivery.
type PairInvitationNotification struct {
	ID                uuid.UUID       `json:"id"`
	InvitationID      uuid.UUID       `json:"invitation_id"`
	InvitationVersion int64           `json:"invitation_version"`
	RecipientID       uuid.UUID       `json:"recipient_id"`
	Kind              string          `json:"kind"`
	Version           int64           `json:"version"`
	State             string          `json:"state"`
	AttemptID         uuid.UUID       `json:"attempt_id"`
	Receipt           json.RawMessage `json:"receipt"`
}

type PairInvitationDelivery struct {
	PairInvitationNotification

	Context PairInvitationView `json:"context"`
}

func pairNoticeRecord(row sqlc.ShuttlePairNotification) PairInvitationNotification {
	return PairInvitationNotification{
		ID: row.ID, InvitationID: row.InvitationID, InvitationVersion: row.InvitationVersion,
		RecipientID: row.RecipientID, Kind: row.Kind, Version: row.Version,
		State: row.State, AttemptID: uuid.UUID(row.AttemptID.Bytes), Receipt: row.Receipt,
	}
}

// enqueuePairInvitationNotice runs in the invitation transition's transaction.
// Pending/revoked notices go to the target; decisions and expiry go to the inviter.
func enqueuePairInvitationNotice(ctx context.Context, q *sqlc.Queries, id uuid.UUID) error {
	return q.InsertPairInvitationNotification(ctx, sqlc.InsertPairInvitationNotificationParams{
		ID: uuid.New(), InvitationID: id,
	})
}

func (s *Store) ReadyPairInvitationNotifications(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]PairInvitationNotification, error) {
	rows, err := s.q.ReadyPairInvitationNotifications(ctx, sqlc.ReadyPairInvitationNotificationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, AfterID: after, PageLimit: NotificationRepairPageSize,
	})
	if err != nil {
		return nil, err
	}
	result := make([]PairInvitationNotification, 0, len(rows))
	for _, row := range rows {
		result = append(result, pairNoticeRecord(row))
	}
	return result, nil
}

func (s *Store) PairInvitationNotification(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (PairInvitationDelivery, error) {
	row, err := s.q.GetPairInvitationNotification(ctx, sqlc.GetPairInvitationNotificationParams{
		ID: id, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return PairInvitationDelivery{}, err
	}
	role := registrations.Leader
	if row.InviterRole == registrations.Leader {
		role = registrations.Follower
	}
	return PairInvitationDelivery{
		PairInvitationNotification: pairNoticeRecord(row.ShuttlePairNotification),
		Context: PairInvitationView{
			Invitation: pairInvitationRecord(row.ShuttlePairInvitation), EventName: row.EventName,
			Timezone: row.Timezone, InviterName: row.InviterName, InviterUserID: row.InviterUserID, RequiredRole: role,
			StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC(),
		},
	}, nil
}

func pairNoticeAllowed(ctx context.Context, q *sqlc.Queries, scope EventScope, id uuid.UUID) (bool, error) {
	return q.PairInvitationNotificationAllowed(ctx, sqlc.PairInvitationNotificationAllowedParams{
		ID: id, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
}

func (s *Store) PairNotificationRecipient(
	ctx context.Context,
	scope EventScope,
	n PairInvitationNotification,
) (User, error) {
	allowed, err := pairNoticeAllowed(ctx, s.q, scope, n.ID)
	if err != nil {
		return User{}, err
	}
	if !allowed {
		return User{}, ErrPairDenied
	}
	return s.GetUser(ctx, n.RecipientID)
}

// ReservePairNotification rechecks consent state under the event lock before Telegram IO.
func (s *Store) ReservePairNotification(
	ctx context.Context,
	scope PaymentScope,
	n PairInvitationDelivery,
	payload json.RawMessage,
) (PairInvitationNotification, error) {
	if n.RecipientID != scope.UserID {
		return PairInvitationNotification{}, ErrPairDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairInvitationNotification{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockInvitationActor(ctx, q, scope, n.Context.Invitation.EventID); err != nil {
		return PairInvitationNotification{}, err
	}
	allowed, err := pairNoticeAllowed(ctx, q, scope.EventScope, n.ID)
	if err != nil {
		return PairInvitationNotification{}, err
	}
	if !allowed {
		return PairInvitationNotification{}, ErrPairDenied
	}
	row, err := q.ReservePairInvitationNotification(ctx, sqlc.ReservePairInvitationNotificationParams{
		ID: n.ID, Version: n.Version, AttemptID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Payload: payload,
	})
	if err != nil {
		return PairInvitationNotification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PairInvitationNotification{}, err
	}
	return pairNoticeRecord(row), nil
}

func (s *Store) BlockPairNotification(ctx context.Context, n PairInvitationNotification) error {
	return s.q.BlockPairInvitationNotification(
		ctx,
		sqlc.BlockPairInvitationNotificationParams{ID: n.ID, Version: n.Version},
	)
}

func (s *Store) FinishPairNotification(
	ctx context.Context,
	n PairInvitationNotification,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	switch state {
	case pairNoticeDelivered, pairNoticeRetryable, deliveryUnknown, deliveryFailed:
	default:
		return errors.New("invalid pair notification outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	_, err := s.q.FinishPairInvitationNotification(ctx, sqlc.FinishPairInvitationNotificationParams{
		ID:        n.ID,
		AttemptID: pgtype.UUID{Bytes: n.AttemptID, Valid: true},
		State:     state,
		Receipt:   receipt,
		DelayMs:   delay.Milliseconds(),
	})
	return err
}

const (
	pairNoticeDelivered = "delivered"
	pairNoticeRetryable = "retryable"
)

// RecoverPairNotifications requires the application's exclusive deployment lock.
func (s *Store) RecoverPairNotifications(ctx context.Context, scope EventScope) error {
	return s.q.RecoverPairInvitationNotifications(
		ctx,
		sqlc.RecoverPairInvitationNotificationsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
}

func (s *Store) ListAdminPairNotifications(
	ctx context.Context,
	scope EventScope,
	state string,
	after uuid.UUID,
	limit int32,
) ([]PairInvitationNotification, error) {
	if limit < 1 || limit > 20 {
		return nil, errors.New("invalid pair notification page limit")
	}
	rows, err := s.q.ListAdminPairNotifications(ctx, sqlc.ListAdminPairNotificationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, FilterState: state, AfterID: after, PageLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]PairInvitationNotification, 0, len(rows))
	for _, row := range rows {
		result = append(result, pairNoticeRecord(row))
	}
	return result, nil
}
