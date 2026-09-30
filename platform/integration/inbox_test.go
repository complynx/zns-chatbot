package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func runInboxUntil(t *testing.T, f *fixture, ready func() bool) {
	t.Helper()
	runInboxUntilWithin(t, f, ready, 5*time.Second)
}

func runInboxUntilWithin(t *testing.T, f *fixture, ready func() bool, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()
	require.Eventually(t, ready, wait, 10*time.Millisecond)
}

func completeInbox(t *testing.T, f *fixture, want int64) {
	t.Helper()
	completeInboxWithin(t, f, want, 5*time.Second)
}

func completeInboxWithin(t *testing.T, f *fixture, want int64, wait time.Duration) {
	t.Helper()
	runInboxUntilWithin(t, f, func() bool {
		var cursor int64
		var count int
		err := f.db.QueryRow(t.Context(), `SELECT value,(SELECT count(*) FROM bot.telegram_inbox)
FROM bot.cursors WHERE name='telegram'`).Scan(&cursor, &count)
		return err == nil && cursor == want && count == 0
	}, wait)
}

// These restart scenarios must honor a committed cooldown before using the
// normal completion allowance. The shared immediate-readiness default stays 5s.
func completeInboxAfterCooldown(t *testing.T, f *fixture, want, pendingID int64) {
	t.Helper()
	var due, now time.Time
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT next_attempt_at,clock_timestamp()
 FROM bot.telegram_inbox WHERE update_id=$1 AND state='pending'`, pendingID).Scan(&due, &now))
	wait := max(due.Sub(now), 0) + 5*time.Second
	require.LessOrEqual(t, wait, 15*time.Second, "unexpected cooldown in first-retry scenario")
	completeInboxWithin(t, f, want, wait)
}

func runInboxDatabaseFailure(t *testing.T, f *fixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.ErrorIs(t, f.b.Run(ctx), core.ErrDatabase)
	require.NoError(t, ctx.Err(), "SQL failure must terminate before the caller cancels")
}

func TestInboxPersistsBatchBeforeHandlingAndRecoversWithoutTelegram(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "first"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "second"})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		cancel()
		return agent.Plan{}, context.Canceled
	})
	require.NoError(t, f.b.Run(ctx))
	var pending int
	var received, processed int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram_received'`).Scan(&received),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram'`).Scan(&processed),
	)
	require.Equal(t, 2, pending)
	require.EqualValues(t, 3, received)
	assert.Zero(t, processed)
	// Remove acknowledged upstream updates: restart must use the durable inbox.
	upstream, err := f.b.TG.Updates(t.Context(), received)
	require.NoError(t, err)
	require.Empty(t, upstream)
	var seen []string
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		seen = append(seen, input.Text)
		return agent.Plan{View: agent.OrdersView}, nil
	})
	completeInbox(t, f, received)
	assert.Equal(t, []string{"first", "second"}, seen)
}

func TestInboxBatchFailureDoesNotAdvanceAcknowledgement(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "first"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "second"})
	_, err := f.db.Exec(t.Context(), `CREATE SEQUENCE bot.batch_attempts;
CREATE FUNCTION bot.reject_second() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.update_id=2 THEN PERFORM nextval('bot.batch_attempts');
RAISE EXCEPTION 'synthetic persistence failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_second BEFORE INSERT ON bot.telegram_inbox FOR EACH ROW EXECUTE FUNCTION bot.reject_second()`)
	require.NoError(t, err)
	runInboxDatabaseFailure(t, f)
	var attempts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT last_value FROM bot.batch_attempts`).Scan(&attempts))
	require.Equal(t, 1, attempts, "the failed batch must not retry inside the same runtime")
	var count int
	var received int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&count))
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram_received'`).Scan(&received),
	)
	assert.Zero(t, count, "the entire batch must roll back")
	assert.Zero(t, received)
	assert.Zero(t, f.model.calls, "no processing before durable batch receipt")
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_second ON bot.telegram_inbox`)
	require.NoError(t, err)
	completeInbox(t, f, 3)
}

func TestInboxReplaysCommittedActionWithoutDuplicateEffects(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "Select massage"})
	f.model.plan = agent.Plan{View: "workflow", Action: &agent.Proposal{Name: "select", SlotID: "massage-1"}}
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION bot.reject_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'synthetic completion failure'; END $$;
CREATE TRIGGER reject_completion BEFORE DELETE ON bot.telegram_inbox FOR EACH ROW EXECUTE FUNCTION bot.reject_completion()`)
	require.NoError(t, err)
	runInboxDatabaseFailure(t, f)
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Equal(t, 1, pending)
	current, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	require.EqualValues(t, 1, current.Version)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_completion ON bot.telegram_inbox`)
	require.NoError(t, err)
	completeInbox(t, f, 2)
	after, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, current, after)
	assert.Equal(t, 1, f.model.calls)
	var audits int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.audit WHERE owner='alice'`).Scan(&audits))
	assert.Equal(t, 1, audits)
}

func TestInboxRegistrationPersistenceFailureStopsRuntime(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "first"})
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION core.reject_test_ingress() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'private registration SQL diagnostic'; END $$;
CREATE TRIGGER reject_test_ingress BEFORE INSERT ON core.registration_ingress FOR EACH ROW EXECUTE FUNCTION core.reject_test_ingress()`)
	require.NoError(t, err)
	runInboxDatabaseFailure(t, f)
	var received int64
	var pending, ingress int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT value,
(SELECT count(*) FROM bot.telegram_inbox),(SELECT count(*) FROM core.registration_ingress)
FROM bot.cursors WHERE name='telegram_received'`).Scan(&received, &pending, &ingress))
	require.Zero(t, received)
	require.Zero(t, pending)
	require.Zero(t, ingress)
	require.Zero(t, f.model.calls)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_test_ingress ON core.registration_ingress`)
	require.NoError(t, err)
	completeInbox(t, f, 2)
	require.Equal(t, 1, f.model.calls)
}
