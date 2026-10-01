package passbooking

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

// ErrRegistrationTimeChanged leaves the exact operation pending for a fresh
// attempt. It is not a permanent domain refusal or a database failure.
var ErrRegistrationTimeChanged = errors.New("registration time changed before commit")

// RegistrationClockAttempt observes live domain time throughout one caller-owned
// transaction. Check must follow all writes, including delivery lanes and receipts.
type RegistrationClockAttempt struct {
	clock    registrationingress.Clock
	first    time.Time
	observed bool
	changed  bool
}

// WithClockAttempt returns a scoped service; it never changes the runtime binding.
// A nil clock preserves existing SQL observations and transaction behavior.
func (s Service) WithClockAttempt() (Service, *RegistrationClockAttempt) {
	if s.RegistrationClock == nil {
		return s, nil
	}
	attempt := &RegistrationClockAttempt{clock: s.RegistrationClock}
	s.RegistrationClock = attempt
	return s, attempt
}

func (a *RegistrationClockAttempt) Now(ctx context.Context) (time.Time, error) {
	now, _, err := registrationingress.Observe(ctx, a.clock)
	if err != nil {
		return time.Time{}, err
	}
	if !a.observed {
		a.first, a.observed = now, true
	} else if !now.Equal(a.first) {
		a.changed = true
	}
	return now, nil
}

// Check is the final application observation immediately before outer Commit.
// It makes no SQL calls and does not claim an atomic file/database commit.
func (a *RegistrationClockAttempt) Check(ctx context.Context) error {
	if a == nil || !a.observed {
		return nil
	}
	now, _, err := registrationingress.Observe(ctx, a.clock)
	if err != nil {
		return err
	}
	if a.changed || !now.Equal(a.first) {
		return ErrRegistrationTimeChanged
	}
	return nil
}

// DecisionError also fences a domain refusal before an adapter can record it as
// terminal. Infrastructure errors retain their original authority and identity.
func (a *RegistrationClockAttempt) DecisionError(ctx context.Context, err error) error {
	if a == nil {
		return err
	}
	if err != nil {
		problem, domain := errors.AsType[*core.ProblemError](err)
		if core.IsDatabaseFailure(err) || errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) || !domain || problem.Status >= http.StatusInternalServerError {
			return err
		}
	}
	if changed := a.Check(ctx); changed != nil {
		return changed
	}
	return err
}

// registrationTime is called at the original SQL observation point, after
// locks where required. The default retains its query and error behavior.
func registrationTime(ctx context.Context, tx pgx.Tx, clock registrationingress.Clock) (time.Time, error) {
	observed, configured, err := registrationingress.Observe(ctx, clock)
	if err != nil {
		return time.Time{}, err
	}
	if configured {
		return observed, nil
	}
	var now time.Time
	err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, core.DatabaseOperationError(err)
}

func nullableRegistrationTime(observed *time.Time) pgtype.Timestamptz {
	if observed == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *observed, Valid: true}
}

// registrationSQLTime binds only registration eligibility; nil retains SQL wall time.
func registrationSQLTime(ctx context.Context, clock registrationingress.Clock) (*time.Time, error) {
	now, configured, err := registrationingress.Observe(ctx, clock)
	if err != nil || !configured {
		return nil, err
	}
	return &now, nil
}

// registrationTurnTime observes configured time after the shared turn allocator
// is held. Subsequent rotation cannot wait past this observation.
func registrationTurnTime(ctx context.Context, tx pgx.Tx, clock registrationingress.Clock) (time.Time, error) {
	if clock != nil {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(782619)"); err != nil {
			return time.Time{}, core.DatabaseOperationError(err)
		}
	}
	return registrationTime(ctx, tx, clock)
}
