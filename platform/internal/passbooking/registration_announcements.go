package passbooking

import (
	"context"
	"errors"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

const (
	announcementUncertainResendLimit = 3
	announcementOutcomeUnknown       = "telegram_outcome_unknown"
	announcementRateLimitReason      = "telegram_rate_limit"
)

type RegistrationAnnouncement struct {
	ID       int64  `json:"id"`
	Channel  string `json:"channel"`
	ThreadID *int64 `json:"thread_id"`
	Locale   string `json:"locale"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Text     string `json:"text"`
	Attempts int64  `json:"attempts"`
}

// RegistrationAnnouncementText renders the frozen announcement inputs before sending.
func RegistrationAnnouncementText(item RegistrationAnnouncement) (string, error) {
	// Source thread locales use exact/base then English, not user-locale aliases.
	base, _, _ := strings.Cut(strings.ToLower(item.Locale), "-")
	locale := "en"
	if base == "ru" {
		locale = "ru"
	}
	roleID := i18n.RegistrationAnnouncementFollower
	if item.Role == "leader" {
		roleID = i18n.RegistrationAnnouncementLeader
	}
	role, err := i18n.Translate(locale, roleID, nil)
	if err != nil {
		return "", err
	}
	return i18n.Translate(locale, i18n.RegistrationAnnouncement,
		map[string]string{"name": html.EscapeString(item.Name), "role": role})
}

type AnnouncementCompletion struct {
	ID      int64            `json:"id"`
	Attempt int64            `json:"attempt"`
	Outcome delivery.Outcome `json:"outcome"`
}

// ClaimRegistrationAnnouncement is retained for callers without the shared dispatcher.
func (s Service) ClaimRegistrationAnnouncement(ctx context.Context) (RegistrationAnnouncement, bool, error) {
	if err := s.RecoverRegistrationAnnouncements(ctx); err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return RegistrationAnnouncement{}, false, core.DatabaseOperationError(err)
	}
	entries, err := delivery.Candidates(ctx, tx, s.Delivery.BotID, announcementCandidateLimit)
	_ = tx.Rollback(ctx)
	if err != nil {
		return RegistrationAnnouncement{}, false, err
	}
	for _, entry := range entries {
		if entry.Reference.Owner != delivery.Announcement {
			continue
		}
		id, parseErr := strconv.ParseInt(entry.Reference.Key, 10, 64)
		if parseErr != nil {
			return RegistrationAnnouncement{}, false, parseErr
		}
		item, found, prepareErr := s.PrepareRegistrationAnnouncement(ctx, id)
		if prepareErr != nil || found {
			return item, found, prepareErr
		}
	}
	return RegistrationAnnouncement{}, false, nil
}
func (s Service) BeginRegistrationAnnouncement(
	ctx context.Context,
	attempt delivery.Attempt,
) (delivery.Admission, error) {
	if err := s.Delivery.Validate(); err != nil {
		return delivery.Admission{}, err
	}
	if attempt.ID <= 0 || attempt.Generation <= 0 {
		return delivery.Admission{}, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return delivery.Admission{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	q := dbgen.New(tx)
	row, err := s.lockAnnouncementAdmission(ctx, q, attempt)
	if err != nil {
		return delivery.Admission{}, err
	}
	if row.State != operationPending || !row.LeaseLive {
		return delivery.Admission{}, conflict("pass_announcement_stale")
	}
	if !row.Current {
		return s.cancelAnnouncementAdmission(ctx, tx, q, attempt, clockAttempt)
	}
	if row.LastUncertainAttempt.Valid && row.UncertainResends >= announcementUncertainResendLimit {
		return s.exhaustAnnouncementRetry(ctx, tx, q, attempt, clockAttempt)
	}
	gate, current, err := s.beginCurrentAnnouncement(ctx, tx, q, attempt)
	if err != nil {
		return gate, err
	}
	if !current {
		return s.cancelAnnouncementAdmission(ctx, tx, q, attempt, clockAttempt)
	}
	if !gate.Ready {
		err = s.finishAnnouncement(
			ctx,
			q,
			attempt,
			delivery.Outcome{Kind: delivery.Deferred, Reason: gate.Reason},
			gate.NotBefore,
		)
	} else {
		var count int64
		count, err = q.BeginAnnouncementSend(
			ctx,
			dbgen.BeginAnnouncementSendParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
		)
		err = core.DatabaseOperationError(err)
		if err == nil && count != 1 {
			err = conflict("pass_announcement_stale")
		}
	}
	if err != nil {
		return delivery.Admission{}, err
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return delivery.Admission{}, err
	}
	return gate, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) CompleteRegistrationAnnouncement(ctx context.Context, input AnnouncementCompletion) error {
	if input.ID <= 0 || input.Attempt <= 0 || !input.Outcome.Valid() {
		return invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	q := dbgen.New(tx)
	recorded, err := s.recordAnnouncementTerminalReceipt(ctx, q, input)
	if err != nil {
		return err
	}
	if recorded {
		return core.DatabaseOperationError(tx.Commit(ctx))
	}
	attempt := delivery.Attempt{ID: input.ID, Generation: input.Attempt}
	var row dbgen.LockAnnouncementAttemptRow
	if input.Outcome.Kind == delivery.Uncertain {
		row, err = s.lockAnnouncementAdmission(ctx, q, attempt)
	} else {
		row, err = q.LockAnnouncementAttempt(ctx, dbgen.LockAnnouncementAttemptParams{
			ID: input.ID, BotID: s.Delivery.BotID, Attempt: input.Attempt,
		})
	}
	if err != nil {
		return announcementAttemptError(core.DatabaseOperationError(err))
	}
	lateSuccess, err := announcementLateCompletion(input, row)
	if err != nil {
		return err
	}
	if err = s.recordAnnouncementUncertainty(ctx, q, input, row); err != nil {
		return err
	}
	if err = s.recordAnnouncementConfirmation(ctx, q, input, row); err != nil {
		return err
	}
	if input.Outcome.Kind == delivery.Uncertain && !row.Current {
		_, err = s.cancelAnnouncementAdmission(ctx, tx, q, attempt, clockAttempt)
		return err
	}
	wireOutcome := input.Outcome
	input.Outcome = announcementRetryOutcome(
		input.Outcome,
		row.UncertainResends,
		row.LastUncertainAttempt.Valid || row.State == string(delivery.Uncertain) ||
			input.Outcome.Kind == delivery.Uncertain,
		s.Delivery,
	)
	outcome, deadline, err := s.finishAnnouncementOutcome(ctx, tx, input.ID, input.Outcome, wireOutcome, lateSuccess)
	if err != nil {
		return err
	}
	if err = s.finishAnnouncement(
		ctx,
		q,
		delivery.Attempt{ID: input.ID, Generation: input.Attempt},
		outcome,
		deadline,
	); err != nil {
		return err
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) exhaustAnnouncementRetry(
	ctx context.Context,
	tx pgx.Tx,
	q *dbgen.Queries,
	attempt delivery.Attempt,
	clockAttempt *RegistrationClockAttempt,
) (delivery.Admission, error) {
	outcome := delivery.Outcome{Kind: delivery.Rejected, Reason: "telegram_uncertain_retry_exhausted"}
	if err := delivery.Project(
		ctx,
		tx,
		s.Delivery.BotID,
		announcementReference(attempt.ID),
		outcome.Kind,
		time.Time{},
	); err != nil {
		return delivery.Admission{}, err
	}
	if err := s.finishAnnouncement(ctx, q, attempt, outcome, time.Now()); err != nil {
		return delivery.Admission{}, err
	}
	if err := clockAttempt.Check(ctx); err != nil {
		return delivery.Admission{}, err
	}
	return delivery.Admission{Reason: outcome.Reason}, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) recordAnnouncementTerminalReceipt(
	ctx context.Context,
	q *dbgen.Queries,
	input AnnouncementCompletion,
) (bool, error) {
	if input.Outcome.Kind != delivery.Succeeded {
		if !announcementConfirmedWireOutcome(input.Outcome) {
			return false, nil
		}
		count, err := q.RecordAnnouncementTerminalConfirmation(ctx, dbgen.RecordAnnouncementTerminalConfirmationParams{
			ID: input.ID, BotID: s.Delivery.BotID, Attempt: input.Attempt,
		})
		return count == 1, core.DatabaseOperationError(err)
	}
	count, err := q.RecordAnnouncementTerminalReceipt(ctx, dbgen.RecordAnnouncementTerminalReceiptParams{
		ID: input.ID, BotID: s.Delivery.BotID, Attempt: input.Attempt, MessageID: input.Outcome.MessageID,
	})
	return count == 1, core.DatabaseOperationError(err)
}

// Only canonical provider observations fence an unresolved wire. Local preflight
// and policy failures use the same outcome kinds but are not provider replies.
func announcementConfirmedWireOutcome(outcome delivery.Outcome) bool {
	switch outcome.Kind {
	case delivery.Succeeded:
		return true
	case delivery.Deferred:
		return outcome.Reason == announcementRateLimitReason
	case delivery.Rejected:
		return outcome.Reason == "telegram_recipient_rejected"
	case delivery.Parked:
		return outcome.Reason == telegramInvalidCooldownReason
	case delivery.Paused:
		return outcome.Reason == "telegram_service_rejected"
	case delivery.Sending, delivery.Cancelled, delivery.Uncertain:
		return false
	default:
		return false
	}
}

func announcementLateCompletion(input AnnouncementCompletion, row dbgen.LockAnnouncementAttemptRow) (bool, error) {
	// A confirmed reply cannot schedule the same actual generation again.
	if input.Outcome.Kind != delivery.Succeeded && announcementConfirmedWireOutcome(input.Outcome) &&
		row.State == operationPending && !row.LeaseUntil.Valid &&
		row.LastConfirmedAttempt.Valid && row.LastConfirmedAttempt.Int64 >= input.Attempt {
		return false, conflict("pass_announcement_stale")
	}
	if input.Outcome.Kind == delivery.Succeeded && row.LastConfirmedAttempt.Valid &&
		row.LastConfirmedAttempt.Int64 >= input.Attempt {
		return false, conflict("pass_announcement_stale")
	}
	lateSuccess := row.State == operationPending && input.Outcome.Kind == delivery.Succeeded &&
		row.LastUncertainAttempt.Valid && row.LastUncertainAttempt.Int64 == input.Attempt && !row.LeaseUntil.Valid
	if row.State == operationPending && !lateSuccess &&
		(input.Outcome.Kind == delivery.Succeeded || input.Outcome.Kind == delivery.Uncertain) {
		return false, conflict("pass_announcement_stale")
	}
	return lateSuccess, nil
}

func (s Service) recordAnnouncementConfirmation(
	ctx context.Context,
	q *dbgen.Queries,
	input AnnouncementCompletion,
	row dbgen.LockAnnouncementAttemptRow,
) error {
	if !announcementConfirmedWireOutcome(input.Outcome) {
		return nil
	}
	if row.State != string(delivery.Sending) && row.State != string(delivery.Uncertain) &&
		(!row.LastUncertainAttempt.Valid || row.LastUncertainAttempt.Int64 != input.Attempt || row.LeaseUntil.Valid) {
		return nil
	}
	return core.DatabaseOperationError(q.RecordAnnouncementConfirmation(ctx, dbgen.RecordAnnouncementConfirmationParams{
		ID: input.ID, BotID: s.Delivery.BotID, Attempt: input.Attempt,
	}))
}

// Resends count admissions, not proven wire requests. A crash after admission
// consumes the same durable budget as an uncertain HTTP result.
func announcementRetryOutcome(
	outcome delivery.Outcome,
	resends int64,
	active bool,
	settings delivery.Settings,
) delivery.Outcome {
	if !active || (outcome.Kind != delivery.Uncertain && outcome.Kind != delivery.Deferred) {
		return outcome
	}
	if resends >= announcementUncertainResendLimit {
		return delivery.Outcome{Kind: delivery.Rejected, Reason: "telegram_uncertain_retry_exhausted"}
	}
	base := settings.UncertaintyRetryBaseOrDefault()
	seconds := int64(base / time.Second)
	if base%time.Second != 0 {
		seconds++
	}
	seconds *= int64(1) << resends
	providerSeconds := outcome.RetryAfter
	if outcome.Missing {
		providerSeconds = int64(settings.Fallback / time.Second)
		if settings.Fallback%time.Second != 0 {
			providerSeconds++
		}
	}
	return delivery.Outcome{
		Kind:       delivery.Deferred,
		Reason:     outcome.Reason,
		RetryAfter: max(seconds, providerSeconds),
	}
}

func (s Service) finishAnnouncement(
	ctx context.Context,
	q *dbgen.Queries,
	attempt delivery.Attempt,
	outcome delivery.Outcome,
	deadline time.Time,
) error {
	var failures int64
	if outcome.Kind == delivery.Rejected {
		failures = 1
	}
	count, err := q.FinishAnnouncement(
		ctx,
		dbgen.FinishAnnouncementParams{
			ID:      attempt.ID,
			BotID:   s.Delivery.BotID,
			Attempt: attempt.Generation,
			State: string(
				outcome.Kind,
			),
			MessageID:        outcome.MessageID,
			Failure:          outcome.Reason,
			AvailableAt:      pgtype.Timestamptz{Time: deadline, Valid: true},
			FailureIncrement: failures,
		},
	)
	if err == nil && count != 1 {
		return conflict("pass_announcement_stale")
	}
	return core.DatabaseOperationError(err)
}

func (s Service) cancelAnnouncementAdmission(
	ctx context.Context,
	tx pgx.Tx,
	q *dbgen.Queries,
	attempt delivery.Attempt,
	clockAttempt *RegistrationClockAttempt,
) (delivery.Admission, error) {
	outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "announcement_superseded"}
	deadline := time.Now()
	if err := delivery.Project(
		ctx,
		tx,
		s.Delivery.BotID,
		announcementReference(attempt.ID),
		delivery.Cancelled,
		deadline,
	); err != nil {
		return delivery.Admission{}, err
	}
	if err := s.finishAnnouncement(ctx, q, attempt, outcome, deadline); err != nil {
		return delivery.Admission{}, err
	}
	if err := clockAttempt.Check(ctx); err != nil {
		return delivery.Admission{}, err
	}
	return delivery.Admission{Reason: outcome.Reason}, core.DatabaseOperationError(tx.Commit(ctx))
}

// Only configured domain time needs another observation after transport locks.
// A savepoint removes transport reservations on expiry while retaining the
// event, booking and exact attempt locks acquired in the outer transaction.
func (s Service) beginCurrentAnnouncement(
	ctx context.Context,
	tx pgx.Tx,
	q *dbgen.Queries,
	attempt delivery.Attempt,
) (delivery.Admission, bool, error) {
	if s.RegistrationClock == nil {
		gate, err := delivery.Begin(ctx, tx, s.Delivery, announcementReference(attempt.ID))
		return gate, true, err
	}
	reservation, err := tx.Begin(ctx)
	if err != nil {
		return delivery.Admission{}, false, core.DatabaseOperationError(err)
	}
	defer func() { _ = reservation.Rollback(ctx) }()
	gate, err := delivery.Begin(ctx, reservation, s.Delivery, announcementReference(attempt.ID))
	if err != nil {
		return gate, false, err
	}
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return gate, false, err
	}
	row, err := q.LockAnnouncementAttempt(ctx, dbgen.LockAnnouncementAttemptParams{
		ID:         attempt.ID,
		BotID:      s.Delivery.BotID,
		Attempt:    attempt.Generation,
		DomainTime: nullableRegistrationTime(observed),
	})
	if err != nil {
		return gate, false, announcementAttemptError(core.DatabaseOperationError(err))
	}
	if !row.Current {
		return gate, false, core.DatabaseOperationError(reservation.Rollback(ctx))
	}
	return gate, true, core.DatabaseOperationError(reservation.Commit(ctx))
}

func announcementAttemptError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("pass_announcement_stale")
	}
	return err
}

// Lock current domain authority before the outbox attempt, matching mutation lock order.
func (s Service) lockAnnouncementAdmission(
	ctx context.Context,
	q *dbgen.Queries,
	attempt delivery.Attempt,
) (dbgen.LockAnnouncementAttemptRow, error) {
	source, err := q.AnnouncementAttemptSource(
		ctx,
		dbgen.AnnouncementAttemptSourceParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, announcementAttemptError(core.DatabaseOperationError(err))
	}
	if _, err = q.LockAnnouncementEvent(ctx, source.EventID); err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, core.DatabaseOperationError(err)
	}
	if _, err = q.LockAnnouncementBooking(
		ctx,
		dbgen.LockAnnouncementBookingParams(source),
	); err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, core.DatabaseOperationError(err)
	}
	if s.RegistrationClock != nil {
		if _, err = q.LockAnnouncementAttempt(ctx, dbgen.LockAnnouncementAttemptParams{
			ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation,
		}); err != nil {
			return dbgen.LockAnnouncementAttemptRow{}, announcementAttemptError(core.DatabaseOperationError(err))
		}
	}
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, err
	}
	row, err := q.LockAnnouncementAttempt(
		ctx,
		dbgen.LockAnnouncementAttemptParams{
			ID:         attempt.ID,
			BotID:      s.Delivery.BotID,
			Attempt:    attempt.Generation,
			DomainTime: nullableRegistrationTime(observed),
		},
	)
	if err != nil {
		return dbgen.LockAnnouncementAttemptRow{}, announcementAttemptError(core.DatabaseOperationError(err))
	}
	return row, nil
}

func (s Service) recordAnnouncementUncertainty(
	ctx context.Context,
	q *dbgen.Queries,
	input AnnouncementCompletion,
	row dbgen.LockAnnouncementAttemptRow,
) error {
	if input.Outcome.Kind != delivery.Uncertain && row.State != string(delivery.Uncertain) {
		return nil
	}
	reason := input.Outcome.Reason
	if input.Outcome.Kind != delivery.Uncertain {
		reason = row.Failure
		if reason == "" {
			reason = announcementOutcomeUnknown
		}
	}
	return core.DatabaseOperationError(
		q.RecordAnnouncementUncertainty(
			ctx,
			dbgen.RecordAnnouncementUncertaintyParams{
				ID:      input.ID,
				BotID:   s.Delivery.BotID,
				Attempt: input.Attempt,
				Reason:  reason,
			},
		),
	)
}

// Close a recovered late success or retain a confirmed cooldown at exhaustion.
func (s Service) finishAnnouncementOutcome(
	ctx context.Context,
	tx pgx.Tx,
	id int64,
	policy, wire delivery.Outcome,
	lateSuccess bool,
) (delivery.Outcome, time.Time, error) {
	if lateSuccess {
		return delivery.FinishUncertainSuccess(ctx, tx, s.Delivery, announcementReference(id), policy)
	}
	scheduled := policy
	preserveCooldown := policy.Kind != delivery.Deferred && wire.Kind == delivery.Deferred &&
		wire.Reason == announcementRateLimitReason
	if preserveCooldown {
		scheduled = wire
	}
	outcome, deadline, err := delivery.Finish(ctx, tx, s.Delivery, announcementReference(id), scheduled)
	if err != nil {
		return delivery.Outcome{}, time.Time{}, err
	}
	if preserveCooldown {
		outcome = policy
		err = delivery.Project(ctx, tx, s.Delivery.BotID, announcementReference(id), outcome.Kind, deadline)
	}
	return outcome, deadline, err
}
