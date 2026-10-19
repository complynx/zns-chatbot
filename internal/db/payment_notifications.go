package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const NotificationRepairPageSize = 100

// PaymentNotice freezes the business event, not the recipient's permissions.
type PaymentNotice struct {
	EventName string            `json:"event_name"`
	Timezone  string            `json:"timezone"`
	Evidence  payments.Evidence `json:"evidence"`
	Balance   *payments.Balance `json:"balance,omitempty"`
}

type PaymentNotification struct {
	ID            uuid.UUID       `json:"id"`
	EvidenceID    uuid.UUID       `json:"evidence_id"`
	RecipientID   uuid.UUID       `json:"recipient_id"`
	Kind          string          `json:"kind"`
	State         string          `json:"state"`
	Version       int64           `json:"version"`
	Document      PaymentNotice   `json:"document"`
	AttemptID     uuid.UUID       `json:"attempt_id"`
	Payload       json.RawMessage `json:"-"`
	Receipt       json.RawMessage `json:"receipt"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	CreatedAt     time.Time       `json:"created_at"`
}

type NotificationJob struct {
	ID      uuid.UUID
	Version int64
}

func notificationRecord(row sqlc.ShuttlePaymentNotification) (PaymentNotification, error) {
	result := PaymentNotification{
		ID:            row.ID,
		EvidenceID:    row.EvidenceID,
		RecipientID:   row.RecipientID,
		Kind:          row.Kind,
		State:         row.State,
		Version:       row.Version,
		AttemptID:     uuid.UUID(row.AttemptID.Bytes),
		Payload:       row.Payload,
		Receipt:       row.Receipt,
		NextAttemptAt: row.NextAttemptAt.Time.UTC(),
		CreatedAt:     row.CreatedAt.Time.UTC(),
	}
	err := json.Unmarshal(row.Document, &result.Document)
	return result, err
}

func createPaymentNotifications(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	evidence payments.Evidence,
	kind string,
	balance *payments.Balance,
) error {
	event, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: evidence.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return err
	}
	recipients := []uuid.UUID{evidence.UserID}
	if kind == "submitted" {
		recipients, err = q.ListPaymentNotificationRecipients(
			ctx,
			sqlc.ListPaymentNotificationRecipientsParams{
				EventID:    evidence.EventID,
				BotID:      scope.BotID,
				ZitadelOrg: scope.Organization,
			},
		)
		if err != nil {
			return err
		}
	}
	document, err := json.Marshal(
		PaymentNotice{EventName: event.Name, Timezone: event.Timezone, Evidence: evidence, Balance: balance},
	)
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		err = q.InsertPaymentNotification(
			ctx,
			sqlc.InsertPaymentNotificationParams{
				ID:             uuid.New(),
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				EvidenceID:     evidence.ID,
				RecipientID:    recipient,
				Kind:           kind,
				Document:       document,
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PaymentNotification(ctx context.Context, scope EventScope, id uuid.UUID) (PaymentNotification, error) {
	row, err := s.q.GetPaymentNotification(
		ctx,
		sqlc.GetPaymentNotificationParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return PaymentNotification{}, err
	}
	return notificationRecord(row)
}

func (s *Store) ReadyPaymentNotifications(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.q.ReadyPaymentNotifications(
		ctx,
		sqlc.ReadyPaymentNotificationsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			AfterID:        after,
			PageLimit:      NotificationRepairPageSize,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]NotificationJob, len(rows))
	for i, row := range rows {
		result[i] = NotificationJob{row.ID, row.Version}
	}
	return result, nil
}

func paymentNotificationAllowed(ctx context.Context, q *sqlc.Queries, scope EventScope, id uuid.UUID) (bool, error) {
	return q.PaymentNotificationAllowed(
		ctx,
		sqlc.PaymentNotificationAllowedParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
}

// NotificationRecipient checks current event authority and active source/recipient accounts.
func (s *Store) NotificationRecipient(ctx context.Context, scope EventScope, n PaymentNotification) (User, error) {
	allowed, err := paymentNotificationAllowed(ctx, s.q, scope, n.ID)
	if err != nil {
		return User{}, err
	}
	if !allowed {
		return User{}, payments.ErrDenied
	}
	return s.GetUser(ctx, n.RecipientID)
}

// ReserveNotification commits uncertainty before Telegram IO, after the final local access check.
func (s *Store) ReserveNotification(
	ctx context.Context,
	scope PaymentScope,
	n PaymentNotification,
	payload json.RawMessage,
) (PaymentNotification, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentNotification{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if n.RecipientID != scope.UserID {
		return PaymentNotification{}, payments.ErrDenied
	}
	if _, _, err = lockPaymentEvent(ctx, q, scope, n.Document.Evidence.EventID, false); err != nil {
		return PaymentNotification{}, err
	}
	allowed, err := paymentNotificationAllowed(ctx, q, scope.EventScope, n.ID)
	if err != nil {
		return PaymentNotification{}, err
	}
	if !allowed {
		return PaymentNotification{}, payments.ErrDenied
	}
	row, err := q.ReservePaymentNotification(
		ctx,
		sqlc.ReservePaymentNotificationParams{
			ID:        n.ID,
			Version:   n.Version,
			AttemptID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
			Payload:   payload,
		},
	)
	if err != nil {
		return PaymentNotification{}, err
	}
	result, err := notificationRecord(row)
	if err != nil {
		return PaymentNotification{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PaymentNotification{}, err
	}
	return result, nil
}

func (s *Store) BlockNotification(ctx context.Context, n PaymentNotification) error {
	return s.q.BlockPaymentNotification(
		ctx,
		sqlc.BlockPaymentNotificationParams{ID: n.ID, Version: n.Version, State: "delivery_blocked"},
	)
}

func (s *Store) FinishNotification(
	ctx context.Context,
	n PaymentNotification,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	if !notificationOutcomeValid(state) {
		return errors.New("invalid notification outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	_, err := s.q.FinishPaymentNotification(
		ctx,
		sqlc.FinishPaymentNotificationParams{
			ID:        n.ID,
			AttemptID: pgtype.UUID{Bytes: n.AttemptID, Valid: true},
			State:     state,
			Receipt:   receipt,
			DelayMs:   delay.Milliseconds(),
		},
	)
	return err
}

// RecoverNotifications runs only while the application holds its deployment instance lock.
func (s *Store) RecoverNotifications(ctx context.Context, scope EventScope) error {
	_, err := s.q.RecoverPaymentNotifications(
		ctx,
		sqlc.RecoverPaymentNotificationsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return err
}

type NotificationSummary struct {
	ID            uuid.UUID `json:"id"`
	EvidenceID    uuid.UUID `json:"evidence_id"`
	RecipientID   uuid.UUID `json:"recipient_id"`
	Kind          string    `json:"kind"`
	State         string    `json:"state"`
	Version       int64     `json:"version"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Store) ListAdminNotifications(
	ctx context.Context,
	scope EventScope,
	state string,
	after uuid.UUID,
	limit int32,
) ([]NotificationSummary, error) {
	rows, err := s.q.ListAdminPaymentNotifications(
		ctx,
		sqlc.ListAdminPaymentNotificationsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			FilterState:    state,
			AfterID:        after,
			PageLimit:      limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]NotificationSummary, len(rows))
	for i, row := range rows {
		result[i] = NotificationSummary{
			ID:            row.ID,
			EvidenceID:    row.EvidenceID,
			RecipientID:   row.RecipientID,
			Kind:          row.Kind,
			State:         row.State,
			Version:       row.Version,
			NextAttemptAt: row.NextAttemptAt.Time.UTC(),
			CreatedAt:     row.CreatedAt.Time.UTC(),
		}
	}
	return result, nil
}

func notificationOutcomeValid(state string) bool {
	switch state {
	case "delivered", "retryable", deliveryUnknown, deliveryFailed:
		return true
	default:
		return false
	}
}
