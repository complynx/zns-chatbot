package bot

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Only conclusively recorded handler failures count toward quarantine. SQL,
// cancellation, credit configuration, service-wide deferrals and unknown
// process death never consume this budget.
const (
	inboxFailureLimit = 5
	inboxPassLimit    = 100
)

// inboxRetryDelays()[n] follows n recorded failures. The same delay is persisted
// as a restart lease before the handler runs, so a crashing row cannot hot-loop
// ahead of unrelated chats after restart.
func inboxRetryDelays() [4]time.Duration {
	return [4]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
}

// Fixed diagnostics: no payload or handler error text is stored with the row.
const (
	inboxFailureHandler   = "handler"
	inboxFailureMalformed = "malformed"
	inboxFailureMismatch  = "update_mismatch"
)

var errInboxRowChanged = errors.New("telegram inbox row changed outside its poller")

type inboxOutcome uint8

const (
	inboxFinish inboxOutcome = iota + 1 // success or durable terminal plan
	inboxStop                           // SQL, cancellation or credit configuration
	inboxDefer                          // bot-wide Telegram state; no failure recorded
	inboxFail                           // conclusive handler failure
)

type inboxRow struct {
	payload  []byte
	id       int64
	failures int
}

func inboxBackoff(failures int) time.Duration {
	delays := inboxRetryDelays()
	return delays[min(max(failures, 0), len(delays)-1)]
}

// classifyInboxResult inspects typed provenance only, never error text.
func classifyInboxResult(parent context.Context, err error, failures int) (inboxOutcome, pgtype.Timestamptz) {
	if err == nil {
		return inboxFinish, pgtype.Timestamptz{}
	}
	// Positive SQL provenance wins over joined cancellation or domain errors.
	if core.IsDatabaseFailure(err) || parent.Err() != nil {
		return inboxStop, pgtype.Timestamptz{}
	}
	if _, invalid := errors.AsType[creditCutoverError](err); invalid {
		return inboxStop, pgtype.Timestamptz{}
	}
	if errors.Is(err, errHistoryPlanTerminal) || errors.Is(err, errPassPlanTerminal) {
		return inboxFinish, pgtype.Timestamptz{}
	}
	now := time.Now()
	backoff := pgtype.Timestamptz{Time: now.Add(inboxBackoff(failures)), Valid: true}
	if deadline, deferred := inboxServiceDeadline(now, err); deferred {
		if deadline.InfinityModifier == pgtype.Finite && deadline.Time.Before(backoff.Time) {
			deadline = backoff
		}
		return inboxDefer, deadline
	}
	return inboxFail, backoff
}

// inboxServiceDeadline recognizes bot-wide Telegram states only: durable control
// pacing, structured rate limits and Bot API token/route rejection (401/404 are
// never recipient-specific there; file 404 is ErrInvalidDocument). Application
// ProblemError statuses remain ordinary domain outcomes.
func inboxServiceDeadline(now time.Time, err error) (pgtype.Timestamptz, bool) {
	fallback := int64(inboxBackoff(inboxFailureLimit) / time.Second)
	if control, ok := errors.AsType[*telegram.ControlError](err); ok {
		if control.NotBefore.IsZero() {
			return inboxRetryDeadline(now, fallback), true
		}
		return inboxRetryDeadline(control.NotBefore, 0), true
	}
	// Cancellation from a live parent is not evidence against this update.
	if errors.Is(err, context.Canceled) {
		return pgtype.Timestamptz{Time: now, Valid: true}, true
	}
	api, ok := errors.AsType[*telegram.APIError](err)
	if !ok {
		return pgtype.Timestamptz{}, false
	}
	switch api.Code {
	case http.StatusTooManyRequests:
		parameters := api.Parameters
		if parameters.RetryAfterInvalid {
			return inboxRetryDeadline(now, -1), true
		}
		seconds := parameters.RetryAfter
		if !parameters.RetryAfterPresent && seconds == 0 {
			seconds = fallback
		}
		return inboxRetryDeadline(now, seconds), true
	case http.StatusUnauthorized, http.StatusNotFound:
		return inboxRetryDeadline(now, fallback), true
	default:
		return pgtype.Timestamptz{}, false
	}
}

// Invalid or unrepresentable pacing is parked, never shortened into an early retry.
func inboxRetryDeadline(now time.Time, seconds int64) pgtype.Timestamptz {
	deadline, valid := delivery.Deadline(now, seconds)
	if !valid {
		return pgtype.Timestamptz{InfinityModifier: pgtype.Infinity, Valid: true}
	}
	return pgtype.Timestamptz{Time: deadline, Valid: true}
}

func inboxLeaseMicroseconds() []int64 {
	delays := inboxRetryDelays()
	leases := make([]int64, 0, len(delays))
	for _, delay := range delays {
		leases = append(leases, delay.Microseconds())
	}
	return leases
}

// claimInboxRow selects the oldest due pending chat head. An earlier pending
// row of the same chat blocks later rows regardless of its cooldown. The
// restart lease commits before the handler runs; failures stay unchanged.
func (b *Bot) claimInboxRow(ctx context.Context) (inboxRow, bool, error) {
	var row inboxRow
	err := b.DB.QueryRow(ctx, `UPDATE bot.telegram_inbox SET next_attempt_at=clock_timestamp()+
 ($1::bigint[])[LEAST(failures+1,cardinality($1::bigint[]))]*interval '1 microsecond'
WHERE update_id=(SELECT head.update_id FROM bot.telegram_inbox head
 WHERE head.state='pending' AND head.next_attempt_at<=clock_timestamp()
  AND NOT EXISTS(SELECT 1 FROM bot.telegram_inbox earlier WHERE earlier.state='pending'
   AND earlier.chat_key=head.chat_key AND earlier.update_id<head.update_id)
 ORDER BY head.update_id LIMIT 1)
RETURNING update_id,payload,failures`, inboxLeaseMicroseconds()).Scan(&row.id, &row.payload, &row.failures)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, inboxDatabaseError(ctx, err)
	}
	return row, true, nil
}

// updateInboxRow applies one guarded transition. A failed statement records nothing.
func (b *Bot) updateInboxRow(ctx context.Context, statement string, row inboxRow, arguments ...any) error {
	tag, err := b.DB.Exec(ctx, statement, append([]any{row.id, row.failures}, arguments...)...)
	if err != nil {
		return inboxDatabaseError(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return errInboxRowChanged
	}
	return nil
}

func (b *Bot) failInboxRow(ctx context.Context, row inboxRow, deadline pgtype.Timestamptz, cause error) error {
	quarantined := row.failures+1 >= inboxFailureLimit
	if err := b.updateInboxRow(ctx, `UPDATE bot.telegram_inbox SET failures=failures+1,failure=$3,
 state=CASE WHEN $4::boolean THEN 'quarantined' ELSE 'pending' END,
 quarantined_at=CASE WHEN $4::boolean THEN clock_timestamp() END,
 next_attempt_at=$5::timestamptz
WHERE update_id=$1 AND state='pending' AND failures=$2`,
		row, inboxFailureHandler, quarantined, deadline); err != nil {
		return err
	}
	b.logger().WarnContext(ctx, "telegram update failure recorded", "update_id", row.id,
		"failures", row.failures+1, "quarantined", quarantined, "error", cause)
	return nil
}

func (b *Bot) deferInboxRow(ctx context.Context, row inboxRow, deadline pgtype.Timestamptz) error {
	if err := b.updateInboxRow(ctx, `UPDATE bot.telegram_inbox
 SET next_attempt_at=$3::timestamptz
WHERE update_id=$1 AND state='pending' AND failures=$2`, row, deadline); err != nil {
		return err
	}
	b.logger().WarnContext(ctx, "telegram update deferred", "update_id", row.id, "not_before", deadline)
	return nil
}

// quarantineInboxRow isolates a row that must never reach the handler.
func (b *Bot) quarantineInboxRow(ctx context.Context, row inboxRow, reason string) error {
	if err := b.updateInboxRow(ctx, `UPDATE bot.telegram_inbox
 SET state='quarantined',failure=$3,quarantined_at=clock_timestamp()
WHERE update_id=$1 AND state='pending' AND failures=$2`, row, reason); err != nil {
		return err
	}
	b.logger().WarnContext(ctx, "telegram update quarantined", "update_id", row.id, "reason", reason)
	return nil
}

// releaseInboxLease makes a known non-poison outcome immediately due again.
// It is best effort: if SQL is unavailable the lease only delays the retry.
func (b *Bot) releaseInboxLease(ctx context.Context, row inboxRow) {
	cleanup, cancel := deliveryCompletionContext(ctx)
	defer cancel()
	if err := b.updateInboxRow(cleanup, `UPDATE bot.telegram_inbox SET next_attempt_at='-infinity'
WHERE update_id=$1 AND state='pending' AND failures=$2`, row); err != nil {
		b.logger().WarnContext(ctx, "telegram inbox lease release failed", "update_id", row.id, "error", err)
	}
}
