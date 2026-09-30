package bot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestInboxResultClassification(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	rateLimited := func(parameters telegram.ResponseParameters) error {
		return fmt.Errorf("download: %w", &telegram.APIError{Code: http.StatusTooManyRequests, Parameters: parameters})
	}
	delays := inboxRetryDelays()
	lastDelay := delays[len(delays)-1]
	for _, test := range []struct {
		parent   context.Context
		err      error
		name     string
		failures int
		outcome  inboxOutcome
		delay    time.Duration
		parked   bool
	}{
		{name: "success", err: nil, outcome: inboxFinish},
		{name: "sanitized SQL", err: core.ErrDatabase, outcome: inboxStop},
		{name: "marked adapter SQL", err: core.DatabaseFailure(
			&core.ProblemError{Status: http.StatusInternalServerError, Code: "internal_error"}), outcome: inboxStop},
		{name: "driver statement", err: fmt.Errorf("query: %w", &pgconn.PgError{Code: "23505"}), outcome: inboxStop},
		{name: "SQL joined with domain", err: errors.Join(interaction.ErrOrderReadUnavailable, core.ErrDatabase),
			outcome: inboxStop},
		{name: "SQL joined with cancellation", parent: cancelled,
			err: errors.Join(context.Canceled, core.ErrDatabase), outcome: inboxStop},
		{name: "cancelled ordinary", parent: cancelled, err: errors.New("provider"), outcome: inboxStop},
		{name: "cancelled terminal plan", parent: cancelled, err: errHistoryPlanTerminal, outcome: inboxStop},
		{name: "credit configuration", err: fmt.Errorf("bind: %w", errCreditCutoverLegacyLimit), outcome: inboxStop},
		{name: "history terminal", err: errHistoryPlanTerminal, outcome: inboxFinish},
		{name: "pass terminal", err: fmt.Errorf("plan: %w", errPassPlanTerminal), outcome: inboxFinish},
		{name: "retry after", err: rateLimited(telegram.ResponseParameters{RetryAfter: 900}),
			outcome: inboxDefer, delay: 900 * time.Second},
		{name: "retry after above one hour", err: rateLimited(telegram.ResponseParameters{RetryAfter: 7200}),
			outcome: inboxDefer, delay: 2 * time.Hour},
		{name: "missing retry after", err: rateLimited(telegram.ResponseParameters{}),
			outcome: inboxDefer, delay: lastDelay},
		{name: "retry after below backoff", err: rateLimited(telegram.ResponseParameters{RetryAfter: 1}),
			outcome: inboxDefer, delay: inboxRetryDelays()[0]},
		{name: "retry after below later backoff", failures: 3,
			err: rateLimited(telegram.ResponseParameters{RetryAfter: 1}), outcome: inboxDefer, delay: inboxRetryDelays()[3]},
		{name: "retry after overflow", err: rateLimited(telegram.ResponseParameters{RetryAfter: math.MaxInt64}),
			outcome: inboxDefer, parked: true},
		{name: "invalid retry after", err: rateLimited(telegram.ResponseParameters{RetryAfterInvalid: true}),
			outcome: inboxDefer, parked: true},
		{name: "negative retry after", err: rateLimited(telegram.ResponseParameters{RetryAfter: -1}),
			outcome: inboxDefer, parked: true},
		{name: "bot token rejected", err: &telegram.APIError{Code: http.StatusUnauthorized},
			outcome: inboxDefer, delay: lastDelay},
		{name: "bot route missing", err: &telegram.APIError{Code: http.StatusNotFound},
			outcome: inboxDefer, delay: lastDelay},
		{name: "paused control", err: &telegram.ControlError{Reason: "delivery_paused"},
			outcome: inboxDefer, delay: lastDelay},
		{name: "live-parent cancellation", err: fmt.Errorf("model: %w", context.Canceled),
			outcome: inboxDefer, delay: inboxRetryDelays()[0]},
		{name: "domain not found", err: &core.ProblemError{Status: http.StatusNotFound, Code: "not_found"},
			outcome: inboxFail, delay: inboxRetryDelays()[0]},
		{name: "domain unauthorized", err: &core.ProblemError{Status: http.StatusUnauthorized, Code: "unauthorized"},
			outcome: inboxFail, delay: inboxRetryDelays()[0]},
		{name: "unmarked server problem", err: &core.ProblemError{Status: http.StatusInternalServerError,
			Code: "internal_error"}, outcome: inboxFail, delay: inboxRetryDelays()[0]},
		{name: "recipient blocked bot", err: &telegram.APIError{Code: http.StatusForbidden},
			outcome: inboxFail, delay: inboxRetryDelays()[0]},
		{name: "bad request", err: &telegram.APIError{Code: http.StatusBadRequest},
			outcome: inboxFail, delay: inboxRetryDelays()[0]},
		{name: "provider deadline", err: context.DeadlineExceeded, outcome: inboxFail, delay: inboxRetryDelays()[0]},
		{name: "order binding", failures: 2, err: interaction.ErrOrderReadUnavailable,
			outcome: inboxFail, delay: inboxRetryDelays()[2]},
		{name: "backoff saturates", failures: 9, err: errors.New("provider"),
			outcome: inboxFail, delay: lastDelay},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parent := test.parent
			if parent == nil {
				parent = t.Context()
			}
			before := time.Now()
			outcome, deadline := classifyInboxResult(parent, test.err, test.failures)
			assert.Equal(t, test.outcome, outcome)
			assertInboxDeadline(t, deadline, before, test.delay, test.parked)
		})
	}
}

func assertInboxDeadline(
	t *testing.T, deadline pgtype.Timestamptz, before time.Time, delay time.Duration, parked bool,
) {
	t.Helper()
	if parked {
		assert.True(t, deadline.Valid)
		assert.Equal(t, pgtype.Infinity, deadline.InfinityModifier)
		return
	}
	if delay == 0 {
		assert.False(t, deadline.Valid)
		return
	}
	require.True(t, deadline.Valid)
	assert.Equal(t, pgtype.Finite, deadline.InfinityModifier)
	assert.False(t, deadline.Time.Before(before.Add(delay)))
	assert.False(t, deadline.Time.After(time.Now().Add(delay)))
}

func TestInboxControlDeadlineIsHonored(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		notBefore time.Duration
		minimum   time.Duration
		maximum   time.Duration
	}{
		{"future pacing deadline", 20 * time.Minute, 19 * time.Minute, 20 * time.Minute},
		{"past deadline keeps backoff", -time.Minute, inboxRetryDelays()[0], inboxRetryDelays()[0]},
		{"far deadline is preserved", 1000 * time.Hour, 1000*time.Hour - time.Second, 1000 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := &telegram.ControlError{Reason: "delivery_cooldown", NotBefore: time.Now().Add(test.notBefore)}
			before := time.Now()
			outcome, deadline := classifyInboxResult(t.Context(), fmt.Errorf("getFile: %w", err), 0)
			require.Equal(t, inboxDefer, outcome)
			assert.False(t, deadline.Time.Before(before.Add(test.minimum)))
			assert.False(t, deadline.Time.After(time.Now().Add(test.maximum)))
		})
	}
}

func TestInboxRetryScheduleMatchesFailureBudget(t *testing.T) {
	t.Parallel()
	delays := inboxRetryDelays()
	require.Len(t, delays, inboxFailureLimit-1, "the final recorded failure quarantines instead")
	previous := time.Duration(0)
	for _, delay := range delays {
		assert.Greater(t, delay, previous)
		previous = delay
	}
	require.Len(t, inboxLeaseMicroseconds(), len(delays))
}

func TestInboxDeadlinesBeyondDurationRange(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	future := time.Date(2500, time.January, 1, 0, 0, 0, 0, time.UTC)
	deadline := inboxRetryDeadline(now, future.Unix()-now.Unix())
	require.True(t, deadline.Valid)
	assert.Equal(t, pgtype.Finite, deadline.InfinityModifier)
	assert.Equal(t, future, deadline.Time)
	control, deferred := inboxServiceDeadline(now, &telegram.ControlError{NotBefore: future})
	require.True(t, deferred)
	assert.Equal(t, deadline, control)
	unrepresentable := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	parked, deferred := inboxServiceDeadline(now, &telegram.ControlError{NotBefore: unrepresentable})
	require.True(t, deferred)
	assert.True(t, parked.Valid)
	assert.Equal(t, pgtype.Infinity, parked.InfinityModifier)
}

// bindBudget runs inside Handle; raw pool/driver errors must keep SQL origin so
// the inbox never counts them as poison evidence.
func TestBudgetBindingTransportFailuresAreSanitizedSQL(t *testing.T) {
	t.Parallel()
	faulted, _ := faultedStartupPool(t)
	_, err := (&Bot{DB: faulted}).bindBudget(t.Context(), "alice", 1)
	requireSafeDatabaseFailure(t, err)
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	closed, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	closed.Close()
	_, err = (&Bot{DB: closed}).bindBudget(t.Context(), "alice", 1)
	requireSafeDatabaseFailure(t, err)
	outcome, _ := classifyInboxResult(t.Context(), err, 0)
	require.Equal(t, inboxStop, outcome)
}
