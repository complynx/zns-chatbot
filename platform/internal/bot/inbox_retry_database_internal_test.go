package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const (
	inboxRetryChatA = 101
	inboxRetryChatB = 202
	// Seconds of scheduling slack between the Go assertion and the DB clock.
	inboxRetryDelaySlack = 5
)

var errInboxRetrySynthetic = errors.New("synthetic ordinary handler failure")

func inboxRetryPayload(t *testing.T, id, chat int64) string {
	t.Helper()
	raw, err := json.Marshal(telegram.Update{ID: id, Message: &telegram.Message{
		ID: id, Chat: telegram.Chat{ID: chat, Type: "private"},
		From: telegram.User{ID: chat, FirstName: "Synthetic"}, Text: "synthetic",
	}})
	require.NoError(t, err)
	return string(raw)
}

func insertInboxRetryRow(t *testing.T, db *pgxpool.Pool, id int64, payload string) {
	t.Helper()
	_, err := db.Exec(t.Context(), `INSERT INTO bot.telegram_inbox(update_id,payload) VALUES($1,$2::text::jsonb)`,
		id, payload)
	require.NoError(t, err)
}

type inboxRetryState struct {
	State, Failure, Payload  string
	Failures                 int
	Due, Quarantined, Parked bool
	// Seconds until next_attempt_at on the database clock; -1 when unscheduled.
	Delay float64
}

func readInboxRetryState(t *testing.T, db *pgxpool.Pool, id int64) (inboxRetryState, bool) {
	t.Helper()
	var state inboxRetryState
	rows, err := db.Query(t.Context(), `SELECT state,failure,payload::text,failures,
 next_attempt_at<=clock_timestamp(),quarantined_at IS NOT NULL,next_attempt_at='infinity',
 CASE WHEN isfinite(next_attempt_at) THEN EXTRACT(EPOCH FROM next_attempt_at-clock_timestamp())::float8 ELSE -1 END
FROM bot.telegram_inbox WHERE update_id=$1`, id)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		require.NoError(t, rows.Err())
		return state, false
	}
	require.NoError(t, rows.Scan(&state.State, &state.Failure, &state.Payload, &state.Failures,
		&state.Due, &state.Quarantined, &state.Parked, &state.Delay))
	return state, true
}

// Replaces waiting: every pending cooldown or lease becomes due now.
func makeInboxRetryDue(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	_, err := db.Exec(t.Context(), `UPDATE bot.telegram_inbox SET next_attempt_at='-infinity' WHERE state='pending'`)
	require.NoError(t, err)
}

func recordingInboxHandler(calls *[]int64, failures map[int64]error) func(context.Context, telegram.Update) error {
	return func(_ context.Context, update telegram.Update) error {
		*calls = append(*calls, update.ID)
		return failures[update.ID]
	}
}

func TestInboxFailingChatRetainsOrderWhileOtherChatProgresses(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	insertInboxRetryRow(t, db, 10, inboxRetryPayload(t, 10, inboxRetryChatA))
	insertInboxRetryRow(t, db, 11, inboxRetryPayload(t, 11, inboxRetryChatA))
	insertInboxRetryRow(t, db, 12, inboxRetryPayload(t, 12, inboxRetryChatB))
	var calls []int64
	failing := recordingInboxHandler(&calls, map[int64]error{10: errInboxRetrySynthetic})
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), failing))
	assert.Equal(t, []int64{10, 12}, calls, "the unrelated chat progresses in the same pass")
	head, found := readInboxRetryState(t, db, 10)
	require.True(t, found)
	assert.Equal(t, "pending", head.State)
	assert.Equal(t, inboxFailureHandler, head.Failure)
	assert.Equal(t, 1, head.Failures)
	assert.InDelta(t, inboxRetryDelays()[0].Seconds(), head.Delay, inboxRetryDelaySlack)
	later, found := readInboxRetryState(t, db, 11)
	require.True(t, found)
	assert.True(t, later.Due, "the later row is due, so only the chat barrier blocks it")
	assert.Zero(t, later.Failures)
	_, found = readInboxRetryState(t, db, 12)
	assert.False(t, found)

	calls = nil
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), failing),
		"a reconstructed runtime keeps the recorded failure and cooldown")
	assert.Empty(t, calls, "the cooling head blocks its due later row")
	head, _ = readInboxRetryState(t, db, 10)
	assert.Equal(t, 1, head.Failures)

	makeInboxRetryDue(t, db)
	succeeding := recordingInboxHandler(&calls, nil)
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), succeeding))
	assert.Equal(t, []int64{10, 11}, calls, "same-chat order is preserved on retry")
	for _, id := range []int64{10, 11} {
		_, found = readInboxRetryState(t, db, id)
		assert.False(t, found)
	}
}

func TestInboxExhaustionQuarantinesAndReleasesChat(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	poison := inboxRetryPayload(t, 10, inboxRetryChatA)
	insertInboxRetryRow(t, db, 10, poison)
	insertInboxRetryRow(t, db, 11, inboxRetryPayload(t, 11, inboxRetryChatA))
	var calls []int64
	handle := recordingInboxHandler(&calls, map[int64]error{10: errInboxRetrySynthetic})
	for range inboxFailureLimit {
		makeInboxRetryDue(t, db)
		require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), handle))
	}
	assert.Equal(t, append(slices.Repeat([]int64{10}, inboxFailureLimit), 11), calls,
		"the later same-chat row runs only after the head is quarantined")
	state, found := readInboxRetryState(t, db, 10)
	require.True(t, found)
	assert.Equal(t, "quarantined", state.State)
	assert.True(t, state.Quarantined)
	assert.Equal(t, inboxFailureHandler, state.Failure)
	assert.Equal(t, inboxFailureLimit, state.Failures)
	assert.JSONEq(t, poison, state.Payload, "the original payload is retained")
	_, found = readInboxRetryState(t, db, 11)
	assert.False(t, found)

	makeInboxRetryDue(t, db)
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), handle))
	assert.Len(t, calls, inboxFailureLimit+1, "quarantine is terminal: no sixth handler call")
}

func TestInboxInterruptedAttemptKeepsLeaseWithoutBudget(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	insertInboxRetryRow(t, db, 10, inboxRetryPayload(t, 10, inboxRetryChatA))
	insertInboxRetryRow(t, db, 11, inboxRetryPayload(t, 11, inboxRetryChatB))
	// The durable claim commits; the process then dies before any outcome.
	row, found, err := (&Bot{DB: db}).claimInboxRow(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(10), row.id)
	var calls []int64
	handle := recordingInboxHandler(&calls, nil)
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), handle))
	assert.Equal(t, []int64{11}, calls, "the leased row does not hot-loop ahead of other chats")
	state, found := readInboxRetryState(t, db, 10)
	require.True(t, found)
	assert.Equal(t, "pending", state.State)
	assert.Zero(t, state.Failures, "unknown process death is not poison evidence")
	assert.Empty(t, state.Failure)
	assert.InDelta(t, inboxRetryDelays()[0].Seconds(), state.Delay, inboxRetryDelaySlack)
	makeInboxRetryDue(t, db)
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), handle))
	assert.Equal(t, []int64{11, 10}, calls)
}

func TestInboxKnownNonPoisonOutcomesConsumeNoBudget(t *testing.T) {
	t.Parallel()
	requireSQL := func(t *testing.T, err error) {
		t.Helper()
		require.True(t, core.IsDatabaseFailure(err))
	}
	for index, test := range []struct {
		outcome      func(context.CancelFunc) error
		check        func(*testing.T, error)
		name         string
		inaccessible bool
	}{
		{name: "SQL joined with domain", check: requireSQL, outcome: func(context.CancelFunc) error {
			return errors.Join(interaction.ErrOrderReadUnavailable, core.ErrDatabase)
		}},
		{name: "SQL joined with cancellation", check: requireSQL, outcome: func(cancel context.CancelFunc) error {
			cancel()
			return errors.Join(context.Canceled, core.ErrDatabase)
		}},
		{name: "parent cancellation", check: requireInboxCancellation, outcome: func(cancel context.CancelFunc) error {
			cancel()
			return context.Canceled
		}},
		{name: "credit configuration", check: requireInboxCreditConfiguration, outcome: func(context.CancelFunc) error {
			return fmt.Errorf("bind: %w", errCreditCutoverEnforcement)
		}},
		{name: "SQL with inaccessible database", inaccessible: true, check: requireSQL,
			outcome: func(context.CancelFunc) error { return core.ErrDatabase }},
		{name: "cancellation with inaccessible database", inaccessible: true, check: requireInboxCancellation,
			outcome: func(cancel context.CancelFunc) error {
				cancel()
				return context.Canceled
			}},
		{name: "credit configuration with inaccessible database", inaccessible: true,
			check:   requireInboxCreditConfiguration,
			outcome: func(context.CancelFunc) error { return errCreditCutoverEnforcement }},
		{name: "unrecorded ordinary failure", inaccessible: true, check: requireSQL,
			outcome: func(context.CancelFunc) error { return errInboxRetrySynthetic }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			id := int64(100 + index)
			insertInboxRetryRow(t, db, id, inboxRetryPayload(t, id, int64(1000+index)))
			runtime, err := pgxpool.NewWithConfig(t.Context(), db.Config())
			require.NoError(t, err)
			t.Cleanup(runtime.Close)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			err = (&Bot{DB: runtime}).drainInboxWith(ctx, func(context.Context, telegram.Update) error {
				calls++
				if test.inaccessible {
					runtime.Close()
				}
				return test.outcome(cancel)
			})
			test.check(t, err)
			assert.Equal(t, 1, calls)
			state, found := readInboxRetryState(t, db, id)
			require.True(t, found, "the update is retained")
			assert.Equal(t, "pending", state.State)
			assert.Zero(t, state.Failures, "no poison budget is consumed")
			assert.Empty(t, state.Failure)
			if !test.inaccessible {
				assert.True(t, state.Due, "a known non-poison outcome releases the restart lease")
			}
		})
	}
}

func requireInboxCancellation(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
}

func requireInboxCreditConfiguration(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, errCreditCutoverEnforcement)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestInboxPayloadMismatchNeverDispatches(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	tag, err := db.Exec(t.Context(), `UPDATE bot.cursors SET value=0 WHERE name='telegram'`)
	require.NoError(t, err)
	if tag.RowsAffected() == 0 {
		_, err = db.Exec(t.Context(), `INSERT INTO bot.cursors(name,value) VALUES('telegram',0)`)
		require.NoError(t, err)
	}
	foreign := inboxRetryPayload(t, 21, inboxRetryChatA)
	insertInboxRetryRow(t, db, 20, foreign)
	insertInboxRetryRow(t, db, 21, inboxRetryPayload(t, 21, inboxRetryChatB))
	insertInboxRetryRow(t, db, 22, inboxRetryPayload(t, 22, inboxRetryChatA))
	var calls []int64
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), recordingInboxHandler(&calls, nil)))
	assert.Equal(t, []int64{21, 22}, calls, "the foreign payload is never dispatched; each real row runs once")
	state, found := readInboxRetryState(t, db, 20)
	require.True(t, found)
	assert.Equal(t, "quarantined", state.State)
	assert.Equal(t, inboxFailureMismatch, state.Failure)
	assert.Zero(t, state.Failures)
	assert.JSONEq(t, foreign, state.Payload)
	var processed int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram'`).Scan(&processed))
	assert.Equal(t, int64(23), processed)
}

func TestInboxServiceDeferralsAndDomainFailures(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	delays := inboxRetryDelays()
	lastDelay := delays[len(delays)-1]
	rateLimited := func(parameters telegram.ResponseParameters) error {
		return &telegram.APIError{Code: http.StatusTooManyRequests, Parameters: parameters}
	}
	cases := []struct {
		err      error
		delay    time.Duration
		failures int
		parked   bool
	}{
		{rateLimited(telegram.ResponseParameters{RetryAfter: 900}), 900 * time.Second, 0, false},
		{rateLimited(telegram.ResponseParameters{RetryAfter: 7200}), 2 * time.Hour, 0, false},
		{rateLimited(telegram.ResponseParameters{RetryAfter: math.MaxInt64}), 0, 0, true},
		{rateLimited(telegram.ResponseParameters{RetryAfterInvalid: true}), 0, 0, true},
		{&telegram.APIError{Code: http.StatusUnauthorized}, lastDelay, 0, false},
		{&telegram.APIError{Code: http.StatusNotFound}, lastDelay, 0, false},
		{&telegram.ControlError{Reason: "delivery_cooldown", NotBefore: time.Now().Add(20 * time.Minute)},
			20 * time.Minute, 0, false},
		{&telegram.ControlError{Reason: "delivery_cooldown", NotBefore: time.Now().Add(1000 * time.Hour)},
			1000 * time.Hour, 0, false},
		{&core.ProblemError{Status: http.StatusNotFound, Code: "not_found"}, inboxRetryDelays()[0], 1, false},
		{&core.ProblemError{Status: http.StatusUnauthorized, Code: "unauthorized"}, inboxRetryDelays()[0], 1, false},
		{&telegram.APIError{Code: http.StatusForbidden}, inboxRetryDelays()[0], 1, false},
		{interaction.ErrOrderReadUnavailable, inboxRetryDelays()[0], 1, false},
	}
	failures := map[int64]error{}
	for index, test := range cases {
		id := int64(30 + index)
		insertInboxRetryRow(t, db, id, inboxRetryPayload(t, id, int64(3000+index)))
		failures[id] = fmt.Errorf("handler: %w", test.err)
	}
	var calls []int64
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), recordingInboxHandler(&calls, failures)))
	assert.Len(t, calls, len(cases), "every chat is attempted once in the pass")
	for index, test := range cases {
		id := int64(30 + index)
		state, found := readInboxRetryState(t, db, id)
		require.True(t, found, test.err.Error())
		assert.Equal(t, "pending", state.State, test.err.Error())
		assert.Equal(t, test.failures, state.Failures, test.err.Error())
		wantFailure := ""
		if test.failures > 0 {
			wantFailure = inboxFailureHandler
		}
		assert.Equal(t, wantFailure, state.Failure, test.err.Error())
		assert.Equal(t, test.parked, state.Parked, test.err.Error())
		if !test.parked {
			assert.InDelta(t, test.delay.Seconds(), state.Delay, inboxRetryDelaySlack, test.err.Error())
		}
	}
	calls = nil
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), recordingInboxHandler(&calls, failures)))
	assert.Empty(t, calls, "a reconstructed runtime does not retry cooling or parked rows")
}

func TestInboxAbsoluteControlDeadlineSurvivesPersistence(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	insertInboxRetryRow(t, db, 10, inboxRetryPayload(t, 10, inboxRetryChatA))
	insertInboxRetryRow(t, db, 11, inboxRetryPayload(t, 11, inboxRetryChatA))
	insertInboxRetryRow(t, db, 12, inboxRetryPayload(t, 12, inboxRetryChatB))
	future := time.Date(2500, time.January, 1, 0, 0, 0, 0, time.UTC)
	var calls []int64
	handle := recordingInboxHandler(&calls, map[int64]error{
		10: &telegram.ControlError{Reason: "delivery_cooldown", NotBefore: future},
	})
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), handle))
	assert.Equal(t, []int64{10, 12}, calls)
	var stored time.Time
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT next_attempt_at FROM bot.telegram_inbox WHERE update_id=10`).Scan(&stored))
	assert.True(t, future.Equal(stored), "an absolute deadline beyond duration range survives storage")
	calls = nil
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(), handle))
	assert.Empty(t, calls, "the far future head retains its same-chat barrier after reconstruction")
}

func TestInboxUnsupportedUpdatesDoNotShareBarrier(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	insertInboxRetryRow(t, db, 60, `{"update_id":60}`)
	insertInboxRetryRow(t, db, 61, `{"update_id":61}`)
	var calls []int64
	require.NoError(t, (&Bot{DB: db}).drainInboxWith(t.Context(),
		recordingInboxHandler(&calls, map[int64]error{60: errInboxRetrySynthetic})))
	assert.Equal(t, []int64{60, 61}, calls, "null chat keys never serialize unrelated rows")
}

func TestInboxChatKeyDerivation(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	key := func(value int64) *int64 { return &value }
	for index, test := range []struct {
		want    *int64
		payload string
	}{
		{key(inboxRetryChatA), `{"message":{"chat":{"id":101}}}`},
		{key(inboxRetryChatB), `{"callback_query":{"message":{"chat":{"id":202}}},"message":{"chat":{"id":101}}}`},
		{key(303), `{"callback_query":null,"message":{"chat":{"id":303}}}`},
		{nil, `{"callback_query":{"id":"callback"},"message":{"chat":{"id":101}}}`},
		{nil, `{"message":{"chat":{"id":"101"}}}`},
		{nil, `{"message":{"chat":{"id":1.5}}}`},
		{nil, `{"message":{"chat":{"id":1e30}}}`},
		{nil, `{"message":{"chat":{"id":1e400}}}`},
		{nil, `{"message":{"chat":{"id":4503599627370496}}}`},
		{key(4503599627370495), `{"message":{"chat":{"id":4503599627370495}}}`},
		{nil, `{"message":{"chat":{"id":-5}}}`},
		{nil, `{"message":{"chat":{"id":0}}}`},
		{nil, `{"message":{"chat":{"id":{"nested":1}}}}`},
		{nil, `{"message":"text"}`},
		{nil, `{}`},
		{nil, `[1]`},
		{nil, `"scalar"`},
	} {
		id := int64(70 + index)
		insertInboxRetryRow(t, db, id, test.payload)
		var actual *int64
		require.NoError(t, db.QueryRow(t.Context(), `SELECT chat_key FROM bot.telegram_inbox WHERE update_id=$1`, id).
			Scan(&actual))
		assert.Equal(t, test.want, actual, test.payload)
	}
}

// A server-side statement error (42P01) inside bindBudget keeps SQL origin but
// no driver text; the successful legacy binding is an ordinary control.
func TestBudgetBindingStatementFailureIsSanitizedSQL(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	_, err := (&Bot{DB: db}).bindBudget(t.Context(), "alice", 1)
	require.NoError(t, err, "no cutover: legacy binding succeeds without configuration")
	_, err = db.Exec(t.Context(), `ALTER TABLE bot.budget_operations RENAME TO budget_operations_unavailable`)
	require.NoError(t, err)
	_, err = (&Bot{DB: db}).bindBudget(t.Context(), "alice", 2)
	requireSafeDatabaseFailure(t, err)
	outcome, _ := classifyInboxResult(t.Context(), err, 0)
	assert.Equal(t, inboxStop, outcome, "SQL origin is never poison evidence")
}
