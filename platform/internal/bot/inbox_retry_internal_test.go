package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type inboxRegistrationClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *inboxRegistrationClock) Now(context.Context) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now, nil
}

func (c *inboxRegistrationClock) advance(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// The callback's normal menu dispatcher, local authenticated application adapter
// and real registration transaction run together. Telegram rendering is outside
// this proof; the durable inbox must retain the input before any success rendering.
func TestInboxRegistrationClockCallbackRetriesSameKey(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	var start time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
	clock := &inboxRegistrationClock{now: start}
	settings := delivery.Settings{BotID: 909090}
	deps := appservices.NewServices(db, appservices.Options{RegistrationClock: clock, Delivery: settings})
	signer := identity.Signer{Key: []byte("synthetic-registration-clock-key-32")}
	server := httptest.NewServer(api.Handler(deps, signer, slog.Default()))
	t.Cleanup(server.Close)
	client := appclient.Client{Base: server.URL, SandboxToken: signer.Token,
		LocalRegistration: &appclient.LocalRegistration{
			Service: deps.Registration,
			Authorizer: applicationauth.Authorizer{
				DB: db,
				Verify: func(_ context.Context, token string) (string, error) {
					return signer.Verify(token)
				},
			},
		}}
	b := &Bot{DB: db, API: client, Host: appclient.Host{Base: server.URL, Signer: signer, UserToken: client.UserToken}}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('dance',now()+interval '1 hour');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',0,20,100,now()-interval '1 hour');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','bob');
 INSERT INTO core.pass_booking_admins(owner) VALUES('bob');
 INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader'),('bob','follower');`,
	)
	require.NoError(t, err)
	price := 0
	action := passMenuAction{
		Assignment: &passbooking.AdminAssignment{Event: "dance", Target: "alice", TotalPrice: &price,
			Create: &passbooking.AdminCreate{FromProfile: true}},
	}
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO bot.pass_buttons(owner,token,revision,action) VALUES('bob','clock-callback',7,$1)`,
		action,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO bot.pass_views(owner,chat_id,revision,state) VALUES('bob',202,7,'{"view":"events"}')`,
	)
	require.NoError(t, err)
	update := telegram.Update{
		ID: 71,
		Callback: &telegram.Callback{ID: "clock-callback", Data: passMenuPrefix + "clock-callback",
			From: telegram.User{ID: 202}, Message: telegram.Message{Chat: telegram.Chat{ID: 202, Type: "private"}}},
	}
	payload, err := json.Marshal(update)
	require.NoError(t, err)
	insertInboxRetryRow(t, db, update.ID, string(payload))
	handle := func(ctx context.Context, received telegram.Update) error {
		in, valid := parseUpdate(received)
		if !valid {
			return errors.New("synthetic callback is invalid")
		}
		authenticated, owner, authErr := client.AuthenticateTelegram(ctx, received.Callback.From.ID)
		if authErr != nil {
			return authErr
		}
		ctx, in.owner = authenticated, owner
		saved, revision, readErr := b.passMenuRecord(ctx, in.owner)
		if readErr != nil {
			return readErr
		}
		_, _, dispatchErr := b.passMenuUpdate(ctx, in, received, saved, revision)
		return dispatchErr
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, err = db.Exec(ctx, `INSERT INTO core.delivery_lanes(bot_id,chat) VALUES($1,'101')`, settings.BotID)
	require.NoError(t, err)
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	_, err = blocker.Exec(
		ctx,
		`SELECT next_sequence FROM core.delivery_lanes WHERE bot_id=$1 AND chat='101' FOR UPDATE`,
		settings.BotID,
	)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- b.drainInboxWith(ctx, handle) }()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event_type='Lock' AND query LIKE '%core.delivery_lanes%FOR UPDATE%')`).Scan(&waiting)
		return readErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	next := start.Add(time.Microsecond)
	clock.advance(next)
	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-done)
	state, found := readInboxRetryState(t, db, update.ID)
	require.True(t, found)
	require.Equal(t, "pending", state.State)
	require.Zero(t, state.Failures)
	require.False(t, state.Quarantined)
	require.False(t, state.Due)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.pass_bookings`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&count))
	require.Zero(t, count)
	makeInboxRetryDue(t, db)
	require.NoError(t, b.drainInboxWith(ctx, handle))
	_, found = readInboxRetryState(t, db, update.ID)
	require.False(t, found)
	booking, err := deps.Registration.Get(ctx, "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, "paid", booking.State)
	require.NotNil(t, booking.AssignedAt)
	require.Equal(t, next, *booking.AssignedAt)
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT count(*) FROM core.pass_admin_assignments WHERE actor='bob'`).Scan(&count),
	)
	require.Equal(t, 1, count, "same callback key produces one committed receipt")
}

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
		{name: "registration clock advance", err: passbooking.ErrRegistrationTimeChanged,
			outcome: inboxDefer, delay: inboxRetryDelays()[0]},
		{name: "clock advance at failure limit", failures: inboxFailureLimit,
			err: passbooking.ErrRegistrationTimeChanged, outcome: inboxDefer, delay: lastDelay},
		{name: "clock advance with SQL provenance", err: errors.Join(passbooking.ErrRegistrationTimeChanged, core.ErrDatabase),
			outcome: inboxStop},
		{name: "clock advance during shutdown", parent: cancelled, err: passbooking.ErrRegistrationTimeChanged,
			outcome: inboxStop},
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
