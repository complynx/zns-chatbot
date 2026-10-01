package passbooking

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

func notificationReference(id int64) delivery.Reference {
	return delivery.Reference{Owner: delivery.Passes, Key: strconv.FormatInt(id, 10), Effect: "send"}
}

// PrepareNotification leases only the requested persisted intent. Begin remains
// the authority for current consent, queue head and shared pacing.
func (s Service) PrepareNotification(ctx context.Context, id int64) (Notification, bool, error) {
	if err := s.Delivery.Validate(); err != nil {
		return Notification{}, false, err
	}
	if id <= 0 {
		return Notification{}, false, notificationInvalid()
	}
	q := dbgen.New(s.DB)
	missing, err := q.MissingNotificationIndex(ctx, s.Delivery.BotID)
	if err != nil {
		return Notification{}, false, core.DatabaseOperationError(err)
	}
	if missing {
		return Notification{}, false, delivery.ErrQueueReference
	}
	row, err := q.PrepareNotification(ctx, dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, false, nil
	}
	if err != nil {
		return Notification{}, false, core.DatabaseOperationError(err)
	}
	notice, err := s.notificationProjection(ctx, s.DB, row)
	return notice, err == nil, err
}

// RecoveryNotifications resumes known-sent followups only. It never prepares
// a new primary send, including when an expired primary has no known outcome.
func (s Service) RecoveryNotifications(ctx context.Context) ([]Notification, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	if err := s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	const maximum = 25
	notices := make([]Notification, 0, maximum)
	for len(notices) < maximum {
		row, err := q.PrepareNotification(
			ctx,
			dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID, FollowupOnly: true},
		)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, core.DatabaseOperationError(err)
		}
		notice, err := s.notificationProjection(ctx, s.DB, row)
		if err != nil {
			return nil, err
		}
		notices = append(notices, notice)
	}
	return notices, nil
}

func (s Service) recoverNotificationSends(ctx context.Context) error {
	missing, err := dbgen.New(s.DB).MissingNotificationIndex(ctx, s.Delivery.BotID)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if missing {
		return delivery.ErrQueueReference
	}
	const maximum = 25
	for range maximum {
		recovered, recoverErr := s.recoverNotificationSend(ctx)
		if recoverErr != nil {
			return recoverErr
		}
		if !recovered {
			return nil
		}
	}
	return nil
}

// Each recovered attempt owns a separate transaction, so recovery never acquires
// a second owner row after locking the first row's delivery lane.
func (s Service) recoverNotificationSend(ctx context.Context) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	q := dbgen.New(tx)
	candidate, err := q.ExpiredNotification(ctx, s.Delivery.BotID)
	if notificationRetryMissing(ctx, err) {
		return false, nil
	}
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	// Candidate selection does not hold the owner row ahead of its source locks.
	current, err := s.lockNotificationEligibility(ctx, tx, candidate.ID)
	if err != nil && !notificationRetryMissing(ctx, err) {
		return false, err
	}
	row, err := q.LockNotificationAttempt(ctx, dbgen.LockNotificationAttemptParams{
		ID: candidate.ID, BotID: s.Delivery.BotID, Attempt: candidate.DeliveryAttempt,
	})
	if notificationRetryMissing(ctx, err) {
		return true, nil
	}
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if row.DeliveryState != "unknown" && (row.DeliveryState != "sending" || row.LeaseLive) {
		return true, nil
	}
	current, err = s.currentPassportNotification(ctx, tx, row, current)
	if err != nil {
		return false, err
	}
	outcome := delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"}
	if !current {
		count, recordErr := q.RecordNotificationUncertainty(ctx, dbgen.RecordNotificationUncertaintyParams{
			ID: row.ID, BotID: s.Delivery.BotID, Attempt: row.DeliveryAttempt,
		})
		if recordErr != nil {
			return false, core.DatabaseOperationError(recordErr)
		}
		if count != 1 {
			return false, notificationStale()
		}
		outcome = delivery.Outcome{Kind: delivery.Cancelled, Reason: "notification_no_longer_current"}
	}
	attempt := delivery.Attempt{ID: row.ID, Generation: row.DeliveryAttempt}
	if err = s.finishNotification(ctx, tx, attempt, outcome, ""); err != nil {
		return false, err
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return false, err
	}
	return true, core.DatabaseOperationError(tx.Commit(ctx))
}

// collectNotificationRegistration retains intent when trusted routing is absent.
// It only collects new rows; lane registration occurs after all owner writes.
func collectNotificationRegistration(
	ctx context.Context,
	tx pgx.Tx,
	botID, id, chat int64,
	pending *[]delivery.Registration,
) error {
	if botID <= 0 || chat == 0 {
		return core.DatabaseOperationError(dbgen.New(tx).PauseUnroutableNotification(ctx, id))
	}
	*pending = append(
		*pending,
		delivery.Registration{
			Reference:   notificationReference(id),
			Destination: delivery.Destination{Chat: strconv.FormatInt(chat, 10)},
			Class:       delivery.Background,
		},
	)
	return nil
}
