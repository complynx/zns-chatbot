package massage

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/massage/dbgen"
)

func notificationReference(id int64) delivery.Reference {
	return delivery.Reference{Owner: delivery.Massage, Key: strconv.FormatInt(id, 10), Effect: "send"}
}

// PrepareNotification leases only the requested persisted intent. Begin remains
// the authority for current consent, queue head and shared pacing.
func (s Service) PrepareNotification(ctx context.Context, id int64) (DeliveryNotice, bool, error) {
	if err := s.Delivery.Validate(); err != nil {
		return DeliveryNotice{}, false, err
	}
	if id <= 0 {
		return DeliveryNotice{}, false, notificationInvalid()
	}
	q := dbgen.New(s.DB)
	missing, err := q.MissingNotificationIndex(ctx, s.Delivery.BotID)
	if err != nil {
		return DeliveryNotice{}, false, core.DatabaseOperationError(err)
	}
	if missing {
		return DeliveryNotice{}, false, delivery.ErrQueueReference
	}
	row, err := q.PrepareNotification(ctx, dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryNotice{}, false, nil
	}
	if err != nil {
		return DeliveryNotice{}, false, core.DatabaseOperationError(err)
	}
	notice, err := s.notificationProjection(ctx, q, row)
	return notice, err == nil, err
}

// RecoveryNotifications resumes known-sent followups only. It never prepares
// a new primary send, including when an expired primary has no known outcome.
func (s Service) RecoveryNotifications(ctx context.Context) ([]DeliveryNotice, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	if err := s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	const maximum = 25
	notices := make([]DeliveryNotice, 0, maximum)
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
		notice, err := s.notificationProjection(ctx, q, row)
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

// Each expired attempt owns a separate transaction, so recovery never acquires
// a second owner row after locking the first row's delivery lane.
func (s Service) recoverNotificationSend(ctx context.Context) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := dbgen.New(tx).ExpiredNotification(ctx, s.Delivery.BotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	outcome := delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"}
	if err = delivery.Project(
		ctx,
		tx,
		s.Delivery.BotID,
		notificationReference(row.ID),
		outcome.Kind,
		time.Time{},
	); err != nil {
		return false, err
	}
	attempt := delivery.Attempt{ID: row.ID, Generation: row.DeliveryAttempt}
	if err = s.saveNotificationOutcome(ctx, dbgen.New(tx), attempt, outcome, "", time.Time{}, 0); err != nil {
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
