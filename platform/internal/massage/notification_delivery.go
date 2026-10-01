package massage

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
	"github.com/complynx/zns-chatbot/platform/internal/massage/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"
)

const notificationPending = "pending"
const notificationNotFound = "not_found"

const notificationUncertainResendLimit int64 = 3
const notificationNotCurrentReason = "notification_no_longer_current"
const notificationRetryExhaustedReason = "telegram_uncertain_retry_exhausted"

// BeginNotification checks current domain eligibility before reserving shared pacing.
// No domain or ledger lock is held while the adapter contacts Telegram.

func (s Service) BeginNotification(
	ctx context.Context,
	owner string,
	input NotificationAttempt,
) (NotificationAdmission, error) {
	attempt := input.Attempt
	if err := s.Delivery.Validate(); err != nil {
		return NotificationAdmission{}, err
	}
	if attempt.ID <= 0 || attempt.Generation <= 0 {
		return NotificationAdmission{}, notificationInvalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return NotificationAdmission{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := s.lockNotificationEligibility(ctx, tx, attempt.ID)
	if err != nil {
		return NotificationAdmission{}, notificationAttemptError(err)
	}
	q := dbgen.New(tx)
	row, err := q.LockNotificationAttempt(
		ctx,
		dbgen.LockNotificationAttemptParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return NotificationAdmission{}, notificationAttemptError(core.DatabaseOperationError(err))
	}
	if row.Owner != owner {
		return NotificationAdmission{}, &core.ProblemError{Status: http.StatusNotFound, Code: notificationNotFound}
	}
	if row.DeliveryState != notificationPending || !row.LeaseLive {
		return NotificationAdmission{}, notificationStale()
	}
	if !current {
		outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: notificationNotCurrentReason}
		if err = s.finishNotification(ctx, tx, attempt, outcome, ""); err != nil {
			return NotificationAdmission{}, err
		}
		return NotificationAdmission{
			Reason: outcome.Reason,
		}, core.DatabaseOperationError(
			tx.Commit(ctx),
		)
	}
	if row.LastUncertainAttempt.Valid && row.UncertainResends >= notificationUncertainResendLimit {
		exhausted, exhaustErr := s.exhaustNotificationAdmission(ctx, tx, attempt)
		return NotificationAdmission{Admission: exhausted}, exhaustErr
	}
	gate, err := delivery.Begin(
		ctx,
		tx,
		s.Delivery,
		notificationReference(attempt.ID),
	)
	if err != nil {
		return NotificationAdmission{}, err
	}
	if !gate.Ready {
		outcome := delivery.Outcome{Kind: delivery.Deferred, Reason: gate.Reason}
		if err = s.saveNotificationOutcome(ctx, q, attempt, outcome, "", gate.NotBefore, 0); err != nil {
			return NotificationAdmission{}, err
		}
		return NotificationAdmission{Admission: gate}, core.DatabaseOperationError(tx.Commit(ctx))
	}
	wire, err := s.admitNotificationWire(ctx, q, attempt, row.DeliveryWirePayload, input.Wire)
	if err != nil {
		return NotificationAdmission{}, err
	}
	err = core.DatabaseOperationError(tx.Commit(ctx))
	return NotificationAdmission{Admission: gate, Wire: wire}, err
}

func (s Service) exhaustNotificationAdmission(
	ctx context.Context,
	tx pgx.Tx,
	attempt delivery.Attempt,
) (delivery.Admission, error) {
	if err := s.exhaustNotificationRetry(ctx, tx, attempt); err != nil {
		return delivery.Admission{}, err
	}
	gate := delivery.Admission{Reason: notificationRetryExhaustedReason}
	return gate, core.DatabaseOperationError(tx.Commit(ctx))
}

// CompleteNotification retains known wire outcomes even after eligibility changes.
// A late result can resolve the same admitted generation before a new retry begins.
func (s Service) CompleteNotification(ctx context.Context, owner string, result NotificationCompletion) error {
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
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := dbgen.New(tx).
		LockNotificationAttempt(ctx, dbgen.LockNotificationAttemptParams{ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt})
	if err != nil {
		return notificationAttemptError(core.DatabaseOperationError(err))
	}
	if row.Owner != owner {
		return &core.ProblemError{Status: http.StatusNotFound, Code: notificationNotFound}
	}
	if notificationTerminalConfirmation(row, result.Outcome) {
		return s.recordTerminalNotificationConfirmation(ctx, tx, result)
	}
	if notificationTerminalReceipt(row, result.Outcome) {
		return s.recordTerminalNotificationReceipt(ctx, tx, result)
	}
	if result.Outcome.Kind == delivery.Uncertain && row.LastUncertainAttempt.Valid &&
		row.LastUncertainAttempt.Int64 == result.Attempt && row.DeliveryState != string(delivery.Sending) {
		return nil
	}
	if (row.DeliveryState != notificationPending || !row.LeaseLive) &&
		row.DeliveryState == string(result.Outcome.Kind) &&
		row.TelegramMessageID == result.Outcome.MessageID &&
		row.Failure == result.Outcome.Reason &&
		row.DeliveryText == result.Text {
		return nil
	}
	if !notificationOutcomeAllowed(row, result.Outcome.Kind) {
		return notificationStale()
	}
	attempt := delivery.Attempt{ID: result.ID, Generation: result.Attempt}
	if notificationLateSuccess(row, result.Outcome.Kind) {
		if err = s.finishLateNotificationSuccess(ctx, tx, attempt, result.Outcome, result.Text); err != nil {
			return err
		}
	} else if err = s.finishNotification(ctx, tx, attempt, result.Outcome, result.Text); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) recordTerminalNotificationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	result NotificationCompletion,
) error {
	count, err := dbgen.New(tx).RecordTerminalNotificationReceipt(ctx, dbgen.RecordTerminalNotificationReceiptParams{
		ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt, MessageID: result.Outcome.MessageID,
	})
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if count != 1 {
		return notificationStale()
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

// A terminal policy stays terminal; a late receipt records only the known wire ID.
func notificationTerminalReceipt(row dbgen.LockNotificationAttemptRow, outcome delivery.Outcome) bool {
	return (row.DeliveryState == "failed" || row.DeliveryState == string(delivery.Cancelled)) &&
		outcome.Kind == delivery.Succeeded && outcome.MessageID > 0 && !row.LeaseUntil.Valid &&
		row.LastUncertainAttempt.Valid && row.LastUncertainAttempt.Int64 == row.DeliveryAttempt &&
		(!row.LastConfirmedAttempt.Valid || row.LastConfirmedAttempt.Int64 != row.DeliveryAttempt) &&
		(row.TelegramMessageID == 0 || row.TelegramMessageID == outcome.MessageID)
}
func notificationLateSuccess(row dbgen.LockNotificationAttemptRow, kind delivery.Kind) bool {
	return row.DeliveryState == notificationPending && kind == delivery.Succeeded && !row.LeaseUntil.Valid &&
		row.LastUncertainAttempt.Valid && row.LastUncertainAttempt.Int64 == row.DeliveryAttempt &&
		(!row.LastConfirmedAttempt.Valid || row.LastConfirmedAttempt.Int64 != row.DeliveryAttempt)
}

func (s Service) finishLateNotificationSuccess(
	ctx context.Context,
	tx pgx.Tx,
	attempt delivery.Attempt,
	outcome delivery.Outcome,
	text string,
) error {
	result, deadline, err := delivery.FinishUncertainSuccess(
		ctx,
		tx,
		s.Delivery,
		notificationReference(attempt.ID),
		outcome,
	)
	if err != nil {
		return err
	}
	return s.saveNotificationOutcome(ctx, dbgen.New(tx), attempt, result, text, deadline, 0)
}
func notificationOutcomeAllowed(row dbgen.LockNotificationAttemptRow, kind delivery.Kind) bool {
	state := row.DeliveryState
	if notificationLateSuccess(row, kind) {
		return true
	}
	if state != notificationPending && state != string(delivery.Sending) && state != "unknown" {
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
	if err = s.saveNotificationOutcome(ctx, dbgen.New(tx), attempt, result, text, deadline, failures); err != nil {
		return err
	}
	if result.Kind == delivery.Uncertain || result.Kind == delivery.Deferred {
		return s.rescheduleNotification(ctx, tx, attempt, result.Kind == delivery.Uncertain, deadline)
	}
	return nil
}

// notificationRetryMissing accepts only absence, never a joined SQL/cancellation failure.
func notificationRetryMissing(ctx context.Context, err error) bool {
	return ctx.Err() == nil && errors.Is(err, pgx.ErrNoRows) && !core.IsDatabaseFailure(err) &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// rescheduleNotification retains the factual unknown while changing only intent scheduling.
func (s Service) rescheduleNotification(
	ctx context.Context,
	tx pgx.Tx,
	attempt delivery.Attempt,
	uncertain bool,
	providerDeadline time.Time,
) error {
	seconds := int64(s.Delivery.UncertaintyRetryBaseOrDefault() / time.Second)
	if s.Delivery.UncertaintyRetryBaseOrDefault()%time.Second != 0 {
		seconds++
	}
	row, err := dbgen.New(tx).RescheduleUncertainNotification(ctx, dbgen.RescheduleUncertainNotificationParams{
		ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation,
		Uncertain: uncertain, FallbackSeconds: seconds,
		ProviderDeadline: pgtype.Timestamptz{Time: providerDeadline, Valid: true},
	})
	if notificationRetryMissing(ctx, err) {
		return nil
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return delivery.Project(
		ctx,
		tx,
		s.Delivery.BotID,
		notificationReference(attempt.ID),
		delivery.Kind(row.DeliveryState),
		row.AvailableAt.Time,
	)
}

// Exhaustion is an owner policy decision, not a fabricated transport rejection.
func (s Service) exhaustNotificationRetry(ctx context.Context, tx pgx.Tx, attempt delivery.Attempt) error {
	if err := delivery.Project(
		ctx,
		tx,
		s.Delivery.BotID,
		notificationReference(attempt.ID),
		delivery.Rejected,
		time.Time{},
	); err != nil {
		return err
	}
	return s.saveNotificationOutcome(ctx, dbgen.New(tx), attempt,
		delivery.Outcome{Kind: delivery.Rejected, Reason: notificationRetryExhaustedReason}, "", time.Time{}, 0)
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
		Confirmed:   notificationConfirmedOutcome(outcome),
		AvailableAt: pgtype.Timestamptz{Time: deadline, Valid: true}, FailureIncrement: failures})
	if err == nil && count != 1 {
		return notificationStale()
	}
	return core.DatabaseOperationError(err)
}

// CompleteNotificationFollowup does not change the canonical transport outcome.
// A failed archive or refresh retains a visible reason and bounded retry deadline.
func (s Service) CompleteNotificationFollowup(ctx context.Context, owner string, result NotificationFollowup) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	if result.ID <= 0 || result.Attempt <= 0 || !validNotificationFollowup(result) {
		return notificationInvalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	row, err := q.LockNotificationAttempt(
		ctx,
		dbgen.LockNotificationAttemptParams{ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt},
	)
	if err != nil {
		return notificationAttemptError(core.DatabaseOperationError(err))
	}
	if row.Owner != owner {
		return &core.ProblemError{Status: http.StatusNotFound, Code: notificationNotFound}
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
		return core.DatabaseOperationError(err)
	}
	if count != 1 {
		return notificationStale()
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func validNotificationFollowup(result NotificationFollowup) bool {
	if result.Done {
		return result.Failure == "" || result.Failure == "notification_recipient_unavailable" ||
			result.Failure == notificationNotCurrentReason
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

func (s Service) NotificationStatus(ctx context.Context, owner string, id int64) (NotificationDeliveryStatus, error) {
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
		return NotificationDeliveryStatus{}, core.DatabaseOperationError(err)
	}
	if row.Owner != owner {
		return NotificationDeliveryStatus{}, &core.ProblemError{
			Status: http.StatusNotFound,
			Code:   "notification_not_found",
		}
	}
	status := NotificationDeliveryStatus{
		ID:                   row.ID,
		Attempt:              row.DeliveryAttempt,
		State:                row.DeliveryState,
		MessageID:            row.TelegramMessageID,
		Reason:               row.Failure,
		FailureCount:         row.FailureCount,
		AvailableAt:          row.AvailableAt.Time,
		FollowupPending:      row.FollowupPending,
		FollowupFailure:      row.FollowupFailure,
		FollowupAttempts:     row.FollowupAttempts,
		LastUncertainAttempt: row.LastUncertainAttempt.Int64,
		LastUncertainReason:  row.LastUncertainReason.String,
		UncertainResends:     row.UncertainResends,
	}
	if row.LastUncertainRecordedAt.Valid {
		value := row.LastUncertainRecordedAt.Time
		status.LastUncertainRecordedAt = &value
	}
	return status, nil
}

func (s Service) admitNotificationWire(
	ctx context.Context,
	q *dbgen.Queries,
	attempt delivery.Attempt,
	existing []byte,
	candidate *notificationwire.Payload,
) (*notificationwire.Payload, error) {
	stored, present, err := notificationwire.Decode(existing)
	if err != nil {
		return nil, err
	}
	wire := candidate
	if present {
		wire = &stored
	}
	if wire == nil {
		return nil, notificationInvalid()
	}
	encoded, err := wire.Encode()
	if err != nil {
		return nil, notificationInvalid()
	}
	committed, err := q.BeginNotificationSend(ctx, dbgen.BeginNotificationSendParams{
		ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation, WirePayload: encoded,
	})
	if err != nil {
		return nil, notificationAttemptError(core.DatabaseOperationError(err))
	}
	value, present, err := notificationwire.Decode(committed)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, notificationwire.ErrPayload
	}
	return &value, nil
}

// Confirmed wire responses resolve uncertainty for one generation, without erasing its history.
func notificationConfirmedOutcome(outcome delivery.Outcome) bool {
	switch outcome.Kind {
	case delivery.Succeeded:
		return true
	case delivery.Deferred:
		return outcome.Reason == "telegram_rate_limit"
	case delivery.Rejected:
		return outcome.Reason == "telegram_recipient_rejected"
	case delivery.Paused:
		return outcome.Reason == "telegram_service_rejected"
	case delivery.Parked:
		return outcome.Reason == "telegram_invalid_cooldown"
	case delivery.Sending, delivery.Cancelled, delivery.Uncertain:
		return false
	default:
		return false
	}
}

func notificationTerminalConfirmation(row dbgen.LockNotificationAttemptRow, outcome delivery.Outcome) bool {
	return (row.DeliveryState == "failed" || row.DeliveryState == string(delivery.Cancelled)) &&
		outcome.Kind != delivery.Succeeded && notificationConfirmedOutcome(outcome) &&
		!row.LeaseUntil.Valid && row.TelegramMessageID == 0 &&
		row.LastUncertainAttempt.Valid && row.LastUncertainAttempt.Int64 == row.DeliveryAttempt
}

func (s Service) recordTerminalNotificationConfirmation(
	ctx context.Context,
	tx pgx.Tx,
	result NotificationCompletion,
) error {
	count, err := dbgen.New(tx).
		RecordTerminalNotificationConfirmation(ctx, dbgen.RecordTerminalNotificationConfirmationParams{
			ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt,
		})
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if count != 1 {
		return notificationStale()
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
