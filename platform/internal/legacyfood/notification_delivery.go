package legacyfood

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood/dbgen"
)

const notificationPending = "pending"

// BeginNotification checks current domain eligibility before reserving shared pacing.
// No domain or ledger lock is held while the adapter contacts Telegram.

func (s Service) BeginNotification(ctx context.Context, attempt delivery.Attempt) (delivery.Admission, error) {
	if err := s.Delivery.Validate(); err != nil {
		return delivery.Admission{}, err
	}
	if attempt.ID <= 0 || attempt.Generation <= 0 {
		return delivery.Admission{}, notificationInvalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return delivery.Admission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := s.lockNotificationEligibility(ctx, tx, attempt.ID)
	if err != nil {
		return delivery.Admission{}, notificationAttemptError(err)
	}
	q := dbgen.New(tx)
	row, err := q.LockNotificationAttempt(
		ctx,
		dbgen.LockNotificationAttemptParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return delivery.Admission{}, notificationAttemptError(err)
	}

	if row.DeliveryState != notificationPending || !row.LeaseLive {
		return delivery.Admission{}, notificationStale()
	}
	if !current {
		outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "notification_no_longer_current"}
		if err = s.finishNotification(ctx, tx, attempt, outcome, ""); err != nil {
			return delivery.Admission{}, err
		}
		return delivery.Admission{Reason: outcome.Reason}, tx.Commit(ctx)
	}
	gate, err := delivery.Begin(
		ctx,
		tx,
		s.Delivery,
		notificationReference(attempt.ID),
	)
	if err != nil {
		return delivery.Admission{}, err
	}
	if !gate.Ready {
		outcome := delivery.Outcome{Kind: delivery.Deferred, Reason: gate.Reason}
		if err = s.saveNotificationOutcome(ctx, q, attempt, outcome, "", gate.NotBefore, 0); err != nil {
			return delivery.Admission{}, err
		}
		return gate, tx.Commit(ctx)
	}
	count, err := q.BeginNotificationSend(
		ctx,
		dbgen.BeginNotificationSendParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return delivery.Admission{}, err
	}
	if count != 1 {
		return delivery.Admission{}, notificationStale()
	}
	return gate, tx.Commit(ctx)
}

// CompleteNotification retains known wire outcomes even after eligibility changes.
// Unknown attempts can resolve from a late transport response, never from a new send.
func (s Service) CompleteNotification(ctx context.Context, result NotificationCompletion) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	if result.ID <= 0 || result.Attempt <= 0 || !result.Outcome.Valid() {
		return notificationInvalid()
	}
	if (result.Outcome.Kind == delivery.Succeeded) != (result.Text != "") {
		return notificationInvalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := dbgen.New(tx).
		LockNotificationAttempt(ctx, dbgen.LockNotificationAttemptParams{ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt})
	if err != nil {
		return notificationAttemptError(err)
	}

	if (row.DeliveryState != notificationPending || !row.LeaseLive) &&
		row.DeliveryState == string(result.Outcome.Kind) &&
		row.TelegramMessageID == result.Outcome.MessageID &&
		row.Failure == result.Outcome.Reason &&
		row.DeliveryText == result.Text {
		return nil
	}
	if !notificationOutcomeAllowed(row.DeliveryState, result.Outcome.Kind) {
		return notificationStale()
	}
	attempt := delivery.Attempt{ID: result.ID, Generation: result.Attempt}
	if err = s.finishNotification(ctx, tx, attempt, result.Outcome, result.Text); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func notificationOutcomeAllowed(state string, kind delivery.Kind) bool {
	if state != notificationPending && state != "sending" && state != "unknown" {
		return false
	}
	if state == notificationPending && (kind == delivery.Succeeded || kind == delivery.Uncertain) {
		return false
	}
	return kind != delivery.Cancelled || state == notificationPending
}

func (s Service) finishNotification(
	ctx context.Context,
	tx pgx.Tx,
	attempt delivery.Attempt,
	outcome delivery.Outcome,
	text string,
) error {
	if outcome.Kind == delivery.Cancelled {
		if err := delivery.Project(
			ctx,
			tx,
			s.Delivery.BotID,
			notificationReference(attempt.ID),
			outcome.Kind,
			time.Time{},
		); err != nil {
			return err
		}
		return s.saveNotificationOutcome(ctx, dbgen.New(tx), attempt, outcome, text, time.Time{}, 0)
	}
	result, deadline, err := delivery.Finish(
		ctx,
		tx,
		s.Delivery,
		notificationReference(attempt.ID),
		outcome,
	)
	if err != nil {
		return err
	}
	var failures int64
	if result.Kind == delivery.Rejected ||
		(result.Kind == delivery.Deferred && result.Reason != "telegram_rate_limit") {
		failures = 1
	}
	return s.saveNotificationOutcome(ctx, dbgen.New(tx), attempt, result, text, deadline, failures)
}

func (s Service) saveNotificationOutcome(
	ctx context.Context,
	q *dbgen.Queries,
	attempt delivery.Attempt,
	outcome delivery.Outcome,
	text string,
	deadline time.Time,
	failures int64,
) error {
	count, err := q.FinishNotificationDelivery(ctx, dbgen.FinishNotificationDeliveryParams{
		ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation, State: string(outcome.Kind),
		MessageID: outcome.MessageID, Text: text, Failure: outcome.Reason,
		AvailableAt: pgtype.Timestamptz{Time: deadline, Valid: true}, FailureIncrement: failures})
	if err == nil && count != 1 {
		return notificationStale()
	}
	return err
}

// CompleteNotificationFollowup does not change the canonical transport outcome.
// A failed archive or refresh retains a visible reason and bounded retry deadline.
func (s Service) CompleteNotificationFollowup(ctx context.Context, result NotificationFollowup) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	if result.ID <= 0 || result.Attempt <= 0 || !validNotificationFollowup(result) {
		return notificationInvalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	row, err := q.LockNotificationAttempt(
		ctx,
		dbgen.LockNotificationAttemptParams{ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt},
	)
	if err != nil {
		return notificationAttemptError(err)
	}

	if row.DeliveryState != "sent" || row.TelegramMessageID <= 0 {
		return notificationStale()
	}
	if !row.FollowupPending && result.Done {
		return nil
	}
	if !row.FollowupPending {
		return notificationStale()
	}
	deadline := row.AvailableAt.Time
	if !result.Done {
		_, deadline, err = delivery.Schedule(
			ctx,
			tx,
			s.Delivery,
			delivery.Destination{Chat: strconv.FormatInt(row.DeliveryChat, 10)},
			delivery.Outcome{Kind: delivery.Deferred, Reason: result.Failure, Missing: true},
		)
		if err != nil {
			return err
		}
	}
	count, err := q.FinishNotificationFollowup(ctx, dbgen.FinishNotificationFollowupParams{
		ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt, Done: result.Done,
		Failure: result.Failure, AvailableAt: pgtype.Timestamptz{Time: deadline, Valid: true}})
	if err != nil {
		return err
	}
	if count != 1 {
		return notificationStale()
	}
	return tx.Commit(ctx)
}

func validNotificationFollowup(result NotificationFollowup) bool {
	if result.Done {
		return result.Failure == "" || result.Failure == "notification_recipient_unavailable" ||
			result.Failure == "notification_no_longer_current"
	}
	return result.Failure == "notification_followup_unavailable"
}

func notificationInvalid() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_notification_delivery"}
}
func notificationStale() error {
	return &core.ProblemError{Status: http.StatusConflict, Code: "notification_stale_attempt"}
}
func notificationAttemptError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return notificationStale()
	}
	return err
}

func (s Service) NotificationStatus(ctx context.Context, id int64) (NotificationDeliveryStatus, error) {
	if err := s.Delivery.Validate(); err != nil {
		return NotificationDeliveryStatus{}, err
	}
	if id <= 0 {
		return NotificationDeliveryStatus{}, notificationInvalid()
	}
	row, err := dbgen.New(s.DB).ReadNotification(ctx, dbgen.ReadNotificationParams{ID: id, BotID: s.Delivery.BotID})
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationDeliveryStatus{}, &core.ProblemError{
			Status: http.StatusNotFound,
			Code:   "notification_not_found",
		}
	}
	if err != nil {
		return NotificationDeliveryStatus{}, err
	}

	return NotificationDeliveryStatus{
		ID:               row.ID,
		Attempt:          row.DeliveryAttempt,
		State:            row.DeliveryState,
		MessageID:        row.TelegramMessageID,
		Reason:           row.Failure,
		FailureCount:     row.FailureCount,
		AvailableAt:      row.AvailableAt.Time,
		FollowupPending:  row.FollowupPending,
		FollowupFailure:  row.FollowupFailure,
		FollowupAttempts: row.FollowupAttempts,
	}, nil
}
