package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const (
	adminUncertainResendLimit = 3
	adminRateLimitReason      = "telegram_rate_limit"
)

// Claim retains the legacy entry point while selecting only shared queue heads.
func (s Service) Claim(ctx context.Context) (Delivery, bool, error) {
	if err := s.RecoverDeliveries(ctx); err != nil {
		return Delivery{}, false, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Delivery{}, false, err
	}
	entries, err := delivery.Candidates(ctx, tx, s.Delivery.BotID, queueCandidateLimit)
	_ = tx.Rollback(ctx)
	if err != nil {
		return Delivery{}, false, err
	}
	for _, entry := range entries {
		if entry.Reference.Owner != delivery.Admin {
			continue
		}
		id, parseErr := strconv.ParseInt(entry.Reference.Key, 10, 64)
		if parseErr != nil {
			return Delivery{}, false, parseErr
		}
		item, found, prepareErr := s.PrepareDelivery(ctx, id)
		if prepareErr != nil || found {
			return item, found, prepareErr
		}
	}
	return Delivery{}, false, nil
}
func (s Service) prepareDelivery(ctx context.Context, candidate dbgen.NextAdminDeliveriesRow) (Delivery, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Delivery{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = guardMessage(ctx, tx, candidate.Actor, candidate.MessageID); err != nil {
		if _, revoked := errors.AsType[*revokedSourceError](err); revoked {
			return Delivery{}, false, tx.Commit(ctx)
		}
		return Delivery{}, false, err
	}
	q := dbgen.New(tx)
	row, err := q.LockAdminDelivery(ctx, dbgen.LockAdminDeliveryParams{ID: candidate.ID, BotID: s.Delivery.BotID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, false, tx.Commit(ctx)
	}
	if err != nil {
		return Delivery{}, false, err
	}
	item := Delivery{
		ID:              row.ID,
		MessageID:       row.MessageID,
		Actor:           row.Actor,
		ActorTelegramID: row.TelegramID,
		State:           row.State,
	}
	if err = json.Unmarshal(row.Destination, &item.Destination); err != nil {
		return Delivery{}, false, err
	}
	if err = json.Unmarshal(row.Content, &item.Content); err != nil {
		return Delivery{}, false, problem(http.StatusConflict, "admin_message_original_wire_unavailable")
	}
	if err = validateContent(item.Content); err != nil {
		return Delivery{}, false, problem(http.StatusConflict, "admin_message_original_wire_unavailable")
	}
	entry, bindingErr := delivery.ReadReference(ctx, tx, s.Delivery.BotID, adminReference(item.ID))
	if bindingErr != nil {
		return Delivery{}, false, bindingErr
	}
	if entry.Destination.Thread != item.Destination.Thread {
		return Delivery{}, false, delivery.ErrQueueBinding
	}
	item.Destination.Chat = entry.Destination.Chat
	item.Attempt, err = q.PrepareAdminDelivery(ctx, dbgen.PrepareAdminDeliveryParams{ID: item.ID, Content: row.Content})
	if err != nil {
		return Delivery{}, false, err
	}
	return item, true, tx.Commit(ctx)
}

// BeginDelivery is called after external identity authorization, without holding
// its SQL locks. It rechecks source/rights and reserves pacing before dispatch.
func (s Service) BeginDelivery(ctx context.Context, attempt delivery.Attempt) (delivery.Admission, error) {
	if err := s.Delivery.Validate(); err != nil {
		return delivery.Admission{}, err
	}
	if attempt.ID <= 0 || attempt.Generation <= 0 {
		return delivery.Admission{}, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return delivery.Admission{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	owner, err := q.AdminAttemptOwner(
		ctx,
		dbgen.AdminAttemptOwnerParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return delivery.Admission{}, adminAttemptError(err)
	}
	if err = guardMessage(ctx, tx, owner.Actor, owner.ID); err != nil {
		if _, revoked := errors.AsType[*revokedSourceError](err); revoked {
			return delivery.Admission{Reason: sourceRevoked}, tx.Commit(ctx)
		}
		return delivery.Admission{}, err
	}
	if err = authorize(ctx, tx, owner.Actor); err != nil {
		return delivery.Admission{}, err
	}
	row, err := q.LockAdminAttempt(
		ctx,
		dbgen.LockAdminAttemptParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
	)
	if err != nil {
		return delivery.Admission{}, adminAttemptError(err)
	}
	if row.State != statePending || !row.LeaseLive {
		return delivery.Admission{}, staleAdminAttempt()
	}
	if row.LastUncertainAttempt.Valid && row.UncertainResends >= adminUncertainResendLimit {
		return s.exhaustAdminRetry(ctx, tx, q, attempt)
	}
	gate, err := delivery.Begin(ctx, tx, s.Delivery, adminReference(attempt.ID))
	if err != nil {
		return gate, err
	}
	if !gate.Ready {
		_, err = q.FinishAdminDelivery(
			ctx,
			dbgen.FinishAdminDeliveryParams{
				ID:          attempt.ID,
				BotID:       s.Delivery.BotID,
				Attempt:     attempt.Generation,
				State:       statePending,
				Failure:     gate.Reason,
				AvailableAt: pgtype.Timestamptz{Time: gate.NotBefore, Valid: true},
			},
		)
	} else {
		var count int64
		count, err = q.BeginAdminSend(
			ctx,
			dbgen.BeginAdminSendParams{ID: attempt.ID, BotID: s.Delivery.BotID, Attempt: attempt.Generation},
		)
		if err == nil && count != 1 {
			err = staleAdminAttempt()
		}
	}
	if err != nil {
		return delivery.Admission{}, err
	}
	return gate, tx.Commit(ctx)
}

// Complete retains the existing explicit service completion entry point.
func (s Service) Complete(ctx context.Context, id, attempt, messageID int64, failure string, retry bool) error {
	outcome := delivery.Outcome{Kind: delivery.Succeeded, MessageID: messageID}
	if failure != "" {
		outcome = delivery.Outcome{Kind: delivery.Rejected, Reason: failure}
	}
	if retry {
		outcome = delivery.Outcome{Kind: delivery.Deferred, Reason: failure, Missing: true}
	}
	return s.CompleteDelivery(ctx, Completion{ID: id, Attempt: attempt, Outcome: outcome})
}

// CompleteDelivery records an exact attempt and retains uncertainty before a
// bounded resend. Withdrawn authority cancels the resend, not the observed fact.
func (s Service) CompleteDelivery(ctx context.Context, result Completion) error {
	if result.ID <= 0 || result.Attempt <= 0 || !result.Outcome.Valid() {
		return invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	recorded, err := s.recordAdminTerminalReceipt(ctx, q, result)
	if err != nil {
		return err
	}
	if recorded {
		return tx.Commit(ctx)
	}
	valid, err := s.completionSource(ctx, tx, result.ID, result.Attempt, result.Outcome.Kind == delivery.Uncertain)
	if err != nil {
		return err
	}
	row, err := q.LockAdminAttempt(
		ctx,
		dbgen.LockAdminAttemptParams{ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt},
	)
	if err != nil {
		return adminAttemptError(err)
	}
	lateSuccess, err := adminLateCompletion(result, row)
	if err != nil {
		return err
	}
	if err = s.recordAdminUncertainty(ctx, q, result, row); err != nil {
		return err
	}
	if err = s.recordAdminConfirmation(ctx, q, result, row); err != nil {
		return err
	}
	wireOutcome := result.Outcome
	result.Outcome = adminRetryOutcome(
		result.Outcome,
		row.UncertainResends,
		row.LastUncertainAttempt.Valid || row.State == string(delivery.Uncertain) ||
			result.Outcome.Kind == delivery.Uncertain,
		s.Delivery,
	)
	if !valid && result.Outcome.Kind == delivery.Rejected {
		result.Outcome = delivery.Outcome{Kind: delivery.Cancelled, Reason: sourceRevoked}
	}
	outcome, deadline, err := s.finishAdminOutcome(ctx, tx, result.ID, result.Outcome, wireOutcome, lateSuccess)
	if err != nil {
		return err
	}
	outcome, err = s.projectAdminSource(ctx, tx, result.ID, outcome, deadline, valid)
	if err != nil {
		return err
	}
	if err = s.finishDelivery(ctx, q, result.ID, result.Attempt, outcome, deadline); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// The original send is outside this budget. Each admitted resend may cross the
// wire even if the process dies before the HTTP call can be observed.
func adminRetryOutcome(
	outcome delivery.Outcome,
	resends int64,
	active bool,
	settings delivery.Settings,
) delivery.Outcome {
	if !active || (outcome.Kind != delivery.Uncertain && outcome.Kind != delivery.Deferred) {
		return outcome
	}
	if resends >= adminUncertainResendLimit {
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

func adminPublicationCurrent(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	var actor, state string
	if err := tx.QueryRow(ctx, `SELECT actor,state FROM core.admin_messages WHERE id=$1 FOR UPDATE`, id).
		Scan(&actor, &state); err != nil {
		return false, err
	}
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return state == "queued", err
}

func (s Service) finishDelivery(
	ctx context.Context,
	q *dbgen.Queries,
	id, attempt int64,
	outcome delivery.Outcome,
	deadline time.Time,
) error {
	var failures int64
	if outcome.Kind == delivery.Rejected ||
		(outcome.Kind == delivery.Deferred && outcome.Reason != adminRateLimitReason) {
		failures = 1
	}
	count, err := q.FinishAdminDelivery(
		ctx,
		dbgen.FinishAdminDeliveryParams{ID: id, BotID: s.Delivery.BotID, Attempt: attempt,
			State: string(outcome.Kind), MessageID: outcome.MessageID, Failure: outcome.Reason,
			AvailableAt: pgtype.Timestamptz{Time: deadline, Valid: true}, FailureIncrement: failures},
	)
	if err == nil && count != 1 {
		return staleAdminAttempt()
	}
	return err
}

func (s Service) completionSource(
	ctx context.Context,
	tx pgx.Tx,
	id, attempt int64,
	requireCurrent bool,
) (bool, error) {
	owner, err := dbgen.New(tx).
		AdminAttemptOwner(ctx, dbgen.AdminAttemptOwnerParams{ID: id, BotID: s.Delivery.BotID, Attempt: attempt})
	if err != nil {
		return false, adminAttemptError(err)
	}
	source, err := messageSource(ctx, tx, owner.Actor, owner.ID)
	if err != nil {
		return false, err
	}
	if err = source.prelock(ctx, tx, owner.Actor); err != nil {
		return false, err
	}
	valid, err := source.validity(ctx, tx, owner.Actor)
	if err != nil {
		return false, err
	}
	if !valid {
		if err = retireMessage(ctx, tx, owner.ID); err != nil {
			return false, err
		}
	}
	if requireCurrent {
		current, currentErr := adminPublicationCurrent(ctx, tx, owner.ID)
		return valid && current, currentErr
	}
	return valid, nil
}

func (s Service) exhaustAdminRetry(
	ctx context.Context,
	tx pgx.Tx,
	q *dbgen.Queries,
	attempt delivery.Attempt,
) (delivery.Admission, error) {
	outcome := delivery.Outcome{Kind: delivery.Rejected, Reason: "telegram_uncertain_retry_exhausted"}
	if err := delivery.Project(
		ctx,
		tx,
		s.Delivery.BotID,
		adminReference(attempt.ID),
		outcome.Kind,
		time.Time{},
	); err != nil {
		return delivery.Admission{}, err
	}
	if err := s.finishDelivery(ctx, q, attempt.ID, attempt.Generation, outcome, time.Now()); err != nil {
		return delivery.Admission{}, err
	}
	return delivery.Admission{Reason: outcome.Reason}, tx.Commit(ctx)
}

func (s Service) projectAdminSource(
	ctx context.Context,
	tx pgx.Tx,
	id int64,
	outcome delivery.Outcome,
	deadline time.Time,
	valid bool,
) (delivery.Outcome, error) {
	if valid || outcome.Kind == delivery.Succeeded || outcome.Kind == delivery.Uncertain {
		return outcome, nil
	}
	outcome = delivery.Outcome{Kind: delivery.Cancelled, Reason: sourceRevoked}
	return outcome, delivery.Project(ctx, tx, s.Delivery.BotID, adminReference(id), outcome.Kind, deadline)
}

func (s Service) recordAdminTerminalReceipt(ctx context.Context, q *dbgen.Queries, result Completion) (bool, error) {
	if result.Outcome.Kind != delivery.Succeeded {
		if !adminConfirmedWireOutcome(result.Outcome) {
			return false, nil
		}
		count, err := q.RecordAdminTerminalConfirmation(ctx, dbgen.RecordAdminTerminalConfirmationParams{
			ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt,
		})
		return count == 1, err
	}
	count, err := q.RecordAdminTerminalReceipt(ctx, dbgen.RecordAdminTerminalReceiptParams{
		ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt, MessageID: result.Outcome.MessageID,
	})
	return count == 1, err
}

// Only canonical provider observations fence an unresolved wire. Local preflight
// and policy failures use the same outcome kinds but are not provider replies.
func adminConfirmedWireOutcome(outcome delivery.Outcome) bool {
	switch outcome.Kind {
	case delivery.Succeeded:
		return true
	case delivery.Deferred:
		return outcome.Reason == adminRateLimitReason
	case delivery.Rejected:
		return outcome.Reason == "telegram_recipient_rejected"
	case delivery.Parked:
		return outcome.Reason == "telegram_invalid_cooldown"
	case delivery.Paused:
		return outcome.Reason == "telegram_service_rejected"
	case delivery.Sending, delivery.Cancelled, delivery.Uncertain:
		return false
	default:
		return false
	}
}

func adminLateCompletion(result Completion, row dbgen.LockAdminAttemptRow) (bool, error) {
	if row.State != statePending && row.State != string(delivery.Sending) && row.State != string(delivery.Uncertain) {
		return false, staleAdminAttempt()
	}
	// A confirmed recovered reply cannot schedule the same generation again.
	if result.Outcome.Kind != delivery.Succeeded && adminConfirmedWireOutcome(result.Outcome) &&
		row.State == statePending && !row.LeaseUntil.Valid &&
		row.LastUncertainAttempt.Valid && row.LastUncertainAttempt.Int64 == result.Attempt &&
		row.LastConfirmedAttempt.Valid && row.LastConfirmedAttempt.Int64 >= result.Attempt {
		return false, staleAdminAttempt()
	}
	if result.Outcome.Kind == delivery.Succeeded && row.LastConfirmedAttempt.Valid &&
		row.LastConfirmedAttempt.Int64 >= result.Attempt {
		return false, staleAdminAttempt()
	}
	lateSuccess := row.State == statePending && result.Outcome.Kind == delivery.Succeeded &&
		row.LastUncertainAttempt.Valid && row.LastUncertainAttempt.Int64 == result.Attempt && !row.LeaseUntil.Valid
	if row.State == statePending && !lateSuccess &&
		(result.Outcome.Kind == delivery.Succeeded || result.Outcome.Kind == delivery.Uncertain) {
		return false, staleAdminAttempt()
	}
	return lateSuccess, nil
}

func (s Service) recordAdminConfirmation(
	ctx context.Context,
	q *dbgen.Queries,
	result Completion,
	row dbgen.LockAdminAttemptRow,
) error {
	if !adminConfirmedWireOutcome(result.Outcome) {
		return nil
	}
	if row.State != string(delivery.Sending) && row.State != string(delivery.Uncertain) &&
		(!row.LastUncertainAttempt.Valid || row.LastUncertainAttempt.Int64 != result.Attempt || row.LeaseUntil.Valid) {
		return nil
	}
	return q.RecordAdminConfirmation(ctx, dbgen.RecordAdminConfirmationParams{
		ID: result.ID, BotID: s.Delivery.BotID, Attempt: result.Attempt,
	})
}

func staleAdminAttempt() error { return problem(http.StatusConflict, "admin_message_stale_attempt") }
func adminAttemptError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return staleAdminAttempt()
	}
	return err
}

func (s Service) recordAdminUncertainty(
	ctx context.Context,
	q *dbgen.Queries,
	result Completion,
	row dbgen.LockAdminAttemptRow,
) error {
	if result.Outcome.Kind != delivery.Uncertain && row.State != string(delivery.Uncertain) {
		return nil
	}
	reason := result.Outcome.Reason
	if result.Outcome.Kind != delivery.Uncertain {
		reason = row.Failure
		if reason == "" {
			reason = "telegram_outcome_unknown"
		}
	}
	return q.RecordAdminUncertainty(
		ctx,
		dbgen.RecordAdminUncertaintyParams{
			ID:      result.ID,
			BotID:   s.Delivery.BotID,
			Attempt: result.Attempt,
			Reason:  reason,
		},
	)
}

// Close a recovered late success or retain a confirmed cooldown at exhaustion.
func (s Service) finishAdminOutcome(
	ctx context.Context,
	tx pgx.Tx,
	id int64,
	policy, wire delivery.Outcome,
	lateSuccess bool,
) (delivery.Outcome, time.Time, error) {
	if lateSuccess {
		return delivery.FinishUncertainSuccess(ctx, tx, s.Delivery, adminReference(id), policy)
	}
	scheduled := policy
	preserveCooldown := policy.Kind != delivery.Deferred && wire.Kind == delivery.Deferred &&
		wire.Reason == adminRateLimitReason
	if preserveCooldown {
		scheduled = wire
	}
	outcome, deadline, err := delivery.Finish(ctx, tx, s.Delivery, adminReference(id), scheduled)
	if err != nil {
		return delivery.Outcome{}, time.Time{}, err
	}
	if preserveCooldown {
		outcome = policy
		err = delivery.Project(ctx, tx, s.Delivery.BotID, adminReference(id), outcome.Kind, deadline)
	}
	return outcome, deadline, err
}
