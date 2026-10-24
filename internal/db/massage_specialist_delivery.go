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

// MassageSpecialistNotification is a durable transition, not a current contact
// permission. Trusted workers must verify IAM and reserve before sending.
type MassageSpecialistNotification struct {
	ID                uuid.UUID
	Booking           MassageBooking
	RecipientID       uuid.UUID
	Kind              string
	ClientName        string
	BindingVersion    int64
	PreferenceVersion int64
	State             string
	Version           int64
	AttemptID         uuid.UUID
	Payload           json.RawMessage
	Receipt           json.RawMessage
	NextAttemptAt     time.Time
	CreatedAt         time.Time
}

func readMassageSpecialistNotification(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	id uuid.UUID,
) (MassageSpecialistNotification, error) {
	row, err := q.ReadMassageSpecialistNotification(
		ctx,
		sqlc.ReadMassageSpecialistNotificationParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return MassageSpecialistNotification{}, err
	}
	booking, err := massageBookingRecord(row.ShuttleMassageBooking)
	if err != nil {
		return MassageSpecialistNotification{}, err
	}
	n := row.ShuttleMassageSpecialistNotification
	return MassageSpecialistNotification{
		ID:                n.ID,
		Booking:           booking,
		RecipientID:       n.RecipientID,
		Kind:              n.Kind,
		ClientName:        n.ClientName,
		BindingVersion:    n.BindingVersion,
		PreferenceVersion: n.PreferenceVersion,
		State:             n.State,
		Version:           n.Version,
		AttemptID: uuid.UUID(
			n.AttemptID.Bytes,
		),
		Payload:       n.Payload,
		Receipt:       n.Receipt,
		NextAttemptAt: n.NextAttemptAt.Time.UTC(),
		CreatedAt:     n.CreatedAt.Time.UTC(),
	}, nil
}

func (s *Store) MassageSpecialistNotification(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (MassageSpecialistNotification, error) {
	return readMassageSpecialistNotification(ctx, s.q, scope, id)
}

func (s *Store) ReadyMassageSpecialistNotifications(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.q.ReadyMassageSpecialistNotifications(
		ctx,
		sqlc.ReadyMassageSpecialistNotificationsParams{
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

func (s *Store) MassageSpecialistNotificationRecipient(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (User, error) {
	row, err := s.q.MassageSpecialistNotificationRecipient(
		ctx,
		sqlc.MassageSpecialistNotificationRecipientParams{
			ID:             id,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	return user(row), err
}

// ReserveMassageSpecialistNotification requires fresh IAM verification before
// entry. Event -> recipient locks fence cancellation, binding and preferences.
// A later change cannot retract an already committed external send reservation.
func (s *Store) ReserveMassageSpecialistNotification(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	version int64,
	payload json.RawMessage,
) (MassageSpecialistNotification, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MassageSpecialistNotification{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	n, err := readMassageSpecialistNotification(ctx, q, scope.EventScope, id)
	if err != nil {
		return n, err
	}
	if n.RecipientID != scope.UserID {
		return n, payments.ErrDenied
	}
	if _, _, err = lockPaymentModuleEvent(ctx, q, scope, n.Booking.EventID, "massage", false); err != nil {
		return n, err
	}
	// Refresh after event/recipient waits and all caller network operations. Neither
	// captured generation is rebased, including a disable followed by re-enable.
	recipient, err := q.MassageSpecialistNotificationRecipient(
		ctx,
		sqlc.MassageSpecialistNotificationRecipientParams{
			ID:             id,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	if err != nil {
		return n, err
	}
	if recipient.ID != scope.UserID {
		return n, payments.ErrDenied
	}
	n, err = readMassageSpecialistNotification(ctx, q, scope.EventScope, id)
	if err != nil {
		return n, err
	}
	row, err := q.ReserveMassageSpecialistNotification(
		ctx,
		sqlc.ReserveMassageSpecialistNotificationParams{
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

func (s *Store) BlockMassageSpecialistNotification(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
	version int64,
) error {
	if _, err := readMassageSpecialistNotification(ctx, s.q, scope, id); err != nil {
		return err
	}
	_, err := s.q.BlockMassageSpecialistNotification(
		ctx,
		sqlc.BlockMassageSpecialistNotificationParams{ID: id, Version: version},
	)
	return err
}

// FinishMassageSpecialistNotification accepts only the reserved attempt. A safe
// retry retains captured generations and must pass current authorization again.
func (s *Store) FinishMassageSpecialistNotification(
	ctx context.Context,
	n MassageSpecialistNotification,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	if !notificationOutcomeValid(state) || delay < 0 {
		return errors.New("invalid massage specialist notification outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	changed, err := s.q.FinishMassageSpecialistNotification(
		ctx,
		sqlc.FinishMassageSpecialistNotificationParams{
			ID:        n.ID,
			AttemptID: n.AttemptID,
			State:     state,
			Receipt:   receipt,
			DelayMs:   delay.Milliseconds(),
		},
	)
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

// RecoverMassageSpecialistNotifications requires the exclusive service instance
// lock. An inflight attempt may have reached Telegram; recovery never resends it.
func (s *Store) RecoverMassageSpecialistNotifications(ctx context.Context, scope EventScope) error {
	_, err := s.q.RecoverMassageSpecialistNotifications(
		ctx,
		sqlc.RecoverMassageSpecialistNotificationsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return err
}
