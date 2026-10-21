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

// OrderNotification retains the original admission/decision and sold terms.
// Delivery state is independent of money and resource allocation state.
type OrderNotification struct {
	NotificationSummary

	Evidence  OrderPaymentEvidence `json:"evidence"`
	Order     SubmittedOrder       `json:"order"`
	AttemptID uuid.UUID            `json:"attempt_id"`
	Receipt   json.RawMessage      `json:"receipt"`
}

func orderNotificationRecord(row sqlc.ShuttleOrderEvidenceNotice, submitted []byte) (OrderNotification, error) {
	result := OrderNotification{
		ID: row.ID, EvidenceID: row.EvidenceID, RecipientID: row.RecipientID,
		Kind: row.Kind, State: row.State, Version: row.Version,
		NextAttemptAt: row.NextAttemptAt.Time.UTC(), CreatedAt: row.CreatedAt.Time.UTC(),
		AttemptID: uuid.UUID(row.AttemptID.Bytes), Receipt: row.Receipt,
	}
	if err := json.Unmarshal(row.Document, &result.Evidence); err != nil {
		return result, err
	}
	var original OrderSubmissionResult
	if err := json.Unmarshal(submitted, &original); err != nil {
		return result, err
	}
	result.Order = original.Order
	return result, nil
}

func readOrderNotification(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	id uuid.UUID,
) (OrderNotification, error) {
	row, err := q.GetOrderNotification(ctx, sqlc.GetOrderNotificationParams{
		ID: id, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return OrderNotification{}, err
	}
	return orderNotificationRecord(row.ShuttleOrderEvidenceNotice, row.SubmittedResult)
}

func (s *Store) OrderNotification(ctx context.Context, scope EventScope, id uuid.UUID) (OrderNotification, error) {
	return readOrderNotification(ctx, s.q, scope, id)
}

func (s *Store) ReadyOrderNotifications(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.q.ReadyOrderNotifications(ctx, sqlc.ReadyOrderNotificationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, AfterID: after, PageLimit: NotificationRepairPageSize,
	})
	if err != nil {
		return nil, err
	}
	result := make([]NotificationJob, len(rows))
	for i, row := range rows {
		result[i] = NotificationJob{ID: row.ID, Version: row.Version}
	}
	return result, nil
}

// OrderNotificationRecipient uses current module/account/authority state. An
// owner's historical notice does not require a currently assigned pass. A
// superseded admission notice is blocked rather than sent as current status.
func (s *Store) OrderNotificationRecipient(ctx context.Context, scope EventScope, id uuid.UUID) (User, bool, error) {
	return orderNoticeRecipient(ctx, s.q, scope, id)
}

func orderNoticeRecipient(ctx context.Context, q *sqlc.Queries, scope EventScope, id uuid.UUID) (User, bool, error) {
	row, err := q.OrderNotificationRecipient(ctx, sqlc.OrderNotificationRecipientParams{
		ID: id, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	return user(row.ShuttleUser), row.Reviewer, err
}

// ReserveOrderNotification re-reads the canonical target, then serializes with
// event/authority and recipient revocation before committing send uncertainty.
// The caller must verify IAM before this local check and send no earlier.
func (s *Store) ReserveOrderNotification(ctx context.Context, scope PaymentScope,
	id uuid.UUID, version int64, payload json.RawMessage,
) (OrderNotification, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderNotification{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	original, err := readOrderNotification(ctx, q, scope.EventScope, id)
	if err != nil {
		return OrderNotification{}, err
	}
	if original.RecipientID != scope.UserID {
		return OrderNotification{}, payments.ErrDenied
	}
	if _, _, err = lockOrderPaymentEvent(
		ctx,
		q,
		scope,
		original.Evidence.EventID,
		original.Evidence.ID,
		false,
	); err != nil {
		return OrderNotification{}, err
	}
	if _, _, err = orderNoticeRecipient(ctx, q, scope.EventScope, id); err != nil {
		return OrderNotification{}, err
	}
	row, err := q.ReserveOrderNotification(ctx, sqlc.ReserveOrderNotificationParams{
		ID: id, Version: version, AttemptID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Payload: payload,
	})
	if err != nil {
		return OrderNotification{}, err
	}
	original.NotificationSummary.State, original.Version = row.State, row.Version
	original.AttemptID = uuid.UUID(row.AttemptID.Bytes)
	return original, tx.Commit(ctx)
}

func (s *Store) BlockOrderNotification(ctx context.Context, scope EventScope, id uuid.UUID, version int64) error {
	if _, err := readOrderNotification(ctx, s.q, scope, id); err != nil {
		return err
	}
	_, err := s.q.BlockOrderNotification(ctx, sqlc.BlockOrderNotificationParams{ID: id, Version: version})
	return err
}

// FinishOrderNotification accepts only the reserved attempt, never a new send.
func (s *Store) FinishOrderNotification(ctx context.Context, n OrderNotification,
	state string, receipt json.RawMessage, delay time.Duration,
) error {
	if !notificationOutcomeValid(state) {
		return errors.New("invalid order notification outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	changed, err := s.q.FinishOrderNotification(ctx, sqlc.FinishOrderNotificationParams{
		ID: n.ID, AttemptID: pgtype.UUID{Bytes: n.AttemptID, Valid: true},
		State: state, Receipt: receipt, DelayMs: delay.Milliseconds(),
	})
	if err == nil && changed != 1 {
		return ErrConflict
	}
	return err
}

// RecoverOrderNotifications requires the application's exclusive instance lock.
// A crash after reservation cannot establish that Telegram rejected the send.
func (s *Store) RecoverOrderNotifications(ctx context.Context, scope EventScope) error {
	_, err := s.q.RecoverOrderNotifications(ctx, sqlc.RecoverOrderNotificationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	return err
}

// ListAdminOrderNotifications returns bounded delivery metadata, without payloads.
func (s *Store) ListAdminOrderNotifications(ctx context.Context, scope EventScope,
	state string, after uuid.UUID, limit int32,
) ([]NotificationSummary, error) {
	if limit < 1 || limit > 20 {
		return nil, payments.ErrInvalid
	}
	rows, err := s.q.ListAdminOrderNotifications(ctx, sqlc.ListAdminOrderNotificationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, FilterState: state, AfterID: after, PageLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]NotificationSummary, 0, len(rows))
	for _, r := range rows {
		result = append(result, NotificationSummary{
			ID:            r.ID,
			EvidenceID:    r.EvidenceID,
			RecipientID:   r.RecipientID,
			Kind:          r.Kind,
			State:         r.State,
			Version:       r.Version,
			NextAttemptAt: r.NextAttemptAt.Time.UTC(),
			CreatedAt:     r.CreatedAt.Time.UTC(),
		})
	}
	return result, nil
}
