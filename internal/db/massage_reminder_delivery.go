package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func (s *Store) ReadyMassageReminders(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.q.ReadyMassageReminders(
		ctx,
		sqlc.ReadyMassageRemindersParams{
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
		result[i] = NotificationJob{ID: row.ID, Version: row.Version}
	}
	return result, nil
}

func (s *Store) MassageReminderRecipient(ctx context.Context, scope EventScope, id uuid.UUID) (User, error) {
	row, err := s.q.MassageReminderRecipient(
		ctx,
		sqlc.MassageReminderRecipientParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return user(row), err
}

// ReserveMassageReminder requires current IAM verification before entry. Event
// and owner locks serialize local revocation/cancellation before the send fence.
func (s *Store) ReserveMassageReminder(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	version int64,
	payload json.RawMessage,
) (MassageReminder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MassageReminder{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	n, err := readMassageReminder(ctx, q, scope.EventScope, id)
	if err != nil {
		return n, err
	}
	if n.RecipientID != scope.UserID {
		return n, payments.ErrDenied
	}
	if _, _, err = lockPaymentModuleEvent(ctx, q, scope, n.Booking.EventID, "massage", false); err != nil {
		return n, err
	}
	// Refresh canonical state after event/owner waits; the worker built its payload
	// before IAM/profile IO. Booking terms are immutable; cancellation suppresses it.
	n, err = readMassageReminder(ctx, q, scope.EventScope, id)
	if err != nil {
		return n, err
	}
	if n.Booking.State != massageBookingStateBooked {
		return n, payments.ErrUnavailable
	}
	row, err := q.ReserveMassageReminder(
		ctx,
		sqlc.ReserveMassageReminderParams{
			ID:        id,
			Version:   version,
			AttemptID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
			Payload:   payload,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, ErrConflict
	}
	if err != nil {
		return n, err
	}
	n.State, n.Version, n.AttemptID, n.Payload = row.State, row.Version, uuid.UUID(row.AttemptID.Bytes), row.Payload
	return n, tx.Commit(ctx)
}

func (s *Store) BlockMassageReminder(ctx context.Context, scope EventScope, id uuid.UUID, version int64) error {
	if _, err := readMassageReminder(ctx, s.q, scope, id); err != nil {
		return err
	}
	_, err := s.q.BlockMassageReminder(ctx, sqlc.BlockMassageReminderParams{ID: id, Version: version})
	return err
}

// FinishMassageReminder accepts only one reserved attempt. A safe retry rebases
// current leads under settings SHARE before locking the reminder, preserving the
// ordering used by settings CAS and new-booking materialization.
func (s *Store) FinishMassageReminder(
	ctx context.Context,
	n MassageReminder,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	if !notificationOutcomeValid(state) || delay < 0 {
		return errors.New("invalid massage reminder outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	params := sqlc.FinishMassageReminderParams{
		ID: n.ID, AttemptID: n.AttemptID, State: state, Receipt: receipt,
		DelayMs: delay.Milliseconds(), LeadSeconds: n.LeadSeconds, DueAt: massageTimestamp(n.DueAt),
	}
	if state == deliveryRetryable {
		params, err = rebaseMassageReminderRetry(ctx, q, params)
		if err != nil {
			return err
		}
	}
	changed, err := q.FinishMassageReminder(ctx, params)
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}

// rebaseMassageReminderRetry takes settings SHARE before the reminder lock.
func rebaseMassageReminderRetry(
	ctx context.Context,
	q *sqlc.Queries,
	params sqlc.FinishMassageReminderParams,
) (sqlc.FinishMassageReminderParams, error) {
	long, short, err := readMassageReminderLeads(ctx, q)
	if err != nil {
		return params, err
	}
	row, err := q.LockMassageReminder(ctx, params.ID)
	if err != nil {
		return params, err
	}
	if row.State != "send_inflight" || !row.AttemptID.Valid || uuid.UUID(row.AttemptID.Bytes) != params.AttemptID {
		return params, ErrConflict
	}
	booking, err := q.ReadMassageBookingByID(ctx, row.BookingID)
	if err != nil {
		return params, err
	}
	if booking.State != massageBookingStateBooked {
		params.State = deliveryBlocked
		return params, nil
	}
	params.LeadSeconds = short
	if row.Kind == MassageReminderLong {
		params.LeadSeconds = long
	}
	params.DueAt = massageTimestamp(booking.StartsAt.Time.Add(-time.Duration(params.LeadSeconds) * time.Second))
	return params, nil
}

// RecoverMassageReminders requires the service's exclusive instance lock. An
// inflight attempt may have reached Telegram and must not be automatically sent.
func (s *Store) RecoverMassageReminders(ctx context.Context, scope EventScope) error {
	_, err := s.q.RecoverMassageReminders(
		ctx,
		sqlc.RecoverMassageRemindersParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return err
}

type MassageReminderSummary struct {
	ID            uuid.UUID `json:"id"`
	BookingID     uuid.UUID `json:"booking_id"`
	EventID       uuid.UUID `json:"event_id"`
	RecipientID   uuid.UUID `json:"recipient_id"`
	Kind          string    `json:"kind"`
	State         string    `json:"state"`
	Version       int64     `json:"version"`
	LeadSeconds   int32     `json:"lead_seconds"`
	DueAt         time.Time `json:"due_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Store) ListAdminMassageReminders(
	ctx context.Context,
	scope EventScope,
	state string,
	after uuid.UUID,
	limit int32,
) ([]MassageReminderSummary, error) {
	if limit < 1 || limit > 20 {
		return nil, payments.ErrInvalid
	}
	rows, err := s.q.ListAdminMassageReminders(
		ctx,
		sqlc.ListAdminMassageRemindersParams{
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
	result := make([]MassageReminderSummary, 0, len(rows))
	for _, n := range rows {
		result = append(
			result,
			MassageReminderSummary{
				ID:            n.ID,
				BookingID:     n.BookingID,
				EventID:       n.EventID,
				RecipientID:   n.RecipientID,
				Kind:          n.Kind,
				State:         n.State,
				Version:       n.Version,
				LeadSeconds:   n.LeadSeconds,
				DueAt:         n.DueAt.Time.UTC(),
				NextAttemptAt: n.NextAttemptAt.Time.UTC(),
				CreatedAt:     n.CreatedAt.Time.UTC(),
			},
		)
	}
	return result, nil
}
