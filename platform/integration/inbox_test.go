package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func runInboxUntil(t *testing.T, f *fixture, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()
	require.Eventually(t, ready, 5*time.Second, 10*time.Millisecond)
}

func completeInbox(t *testing.T, f *fixture, want int64) {
	t.Helper()
	runInboxUntil(t, f, func() bool {
		var cursor int64
		var count int
		err := f.db.QueryRow(t.Context(), `SELECT value,(SELECT count(*) FROM bot.telegram_inbox)
FROM bot.cursors WHERE name='telegram'`).Scan(&cursor, &count)
		return err == nil && cursor == want && count == 0
	})
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
	runInboxUntil(t, f, func() bool {
		var called bool
		queryErr := f.db.QueryRow(t.Context(), `SELECT is_called FROM bot.batch_attempts`).Scan(&called)
		return queryErr == nil && called
	})
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
	runInboxUntil(t, f, func() bool {
		var committed bool
		queryErr := f.db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM core.workflows WHERE owner='alice' AND version=1)`).
			Scan(&committed)
		return queryErr == nil && committed
	})
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
