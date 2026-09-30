package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

// Stop the host before it can persist a final plan or terminal marker.
func TestHistoryInterruptedFinalPlanDoesNotRegenerateAfterDeletion(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"original", "missing_generation", "redacted"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			historyInterruptedFinalPlan(t, mode)
		})
	}
}

func historyInterruptedFinalPlan(t *testing.T, mode string) {
	t.Helper()
	f := setup(t)
	archive := conversation.Service{DB: f.db}
	const canary = "deleted history source canary"
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "terminal-source", "user", canary))
	var eventID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='terminal-source'`).
			Scan(&eventID),
	)
	f.b.Scripts = scopeVM{}
	finalReady := make(chan struct{})
	releaseFinal := make(chan struct{})
	var calls atomic.Int64
	f.b.Model = avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
		if calls.Add(1) == 1 {
			return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{
				Code: fmt.Sprintf(
					`const page=tools.history.read({event_id:%d});tools.preferences.setLanguage({language:"en"});return {text:page.text};`,
					eventID,
				),
				InputJSON: "null",
			}}, nil
		}
		close(finalReady)
		select {
		case <-ctx.Done():
			return agent.Plan{}, ctx.Err()
		case <-releaseFinal:
		}
		return agent.Plan{View: "workflow", Text: canary}, nil
	})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "history race"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "next request"})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	t.Cleanup(cancel)
	select {
	case <-finalReady:
	case <-time.After(5 * time.Second):
		t.Fatal("final plan did not reach barrier")
	}
	require.NoError(t, archive.DeleteContent(t.Context(), "alice", eventID))
	cancel()
	require.NoError(t, <-done)
	require.EqualValues(t, 2, calls.Load())
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Equal(t, 2, pending, "interrupted model must preserve both durable updates")
	// A fresh host must save a terminal marker before acknowledging this update,
	// without a model call or repeated effect, then process the following request.
	restarted := *f.b
	f.b = &restarted
	var followingCalls atomic.Int64
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		followingCalls.Add(1)
		assert.Equal(t, "next request", input.Text)
		raw, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			return agent.Plan{}, encodeErr
		}
		assert.NotContains(t, string(raw), canary)
		return agent.Plan{View: "workflow", Text: "Following request completed"}, nil
	})
	prepareInterruptedHistoryLedger(t, f, mode)
	assertInterruptedMarkerRetry(t, f, &followingCalls)
	recovered := *f.b
	f.b = &recovered
	completeInbox(t, f, 3)
	require.EqualValues(t, 1, followingCalls.Load())
	var effects int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice' AND operation_key LIKE 'tg-script-%'`).
			Scan(&effects),
	)
	assert.Equal(t, 1, effects)
	var saved string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT payload::text FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).
			Scan(&saved),
	)
	var terminal bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind='terminal' AND state='privacy_terminal' AND reason='history_deleted' FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).
			Scan(&terminal),
	)
	assert.True(t, terminal)
	assert.NotContains(t, saved, canary)
	var cursor int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram'`).Scan(&cursor))
	assert.EqualValues(t, 3, cursor)
	// Repeated restarts retain the acknowledgement and cannot run either model.
	restartedAgain := *f.b
	f.b = &restartedAgain
	completeInbox(t, f, 3)
	assert.EqualValues(t, 1, followingCalls.Load())
}

func prepareInterruptedHistoryLedger(t *testing.T, f *fixture, mode string) {
	t.Helper()
	var plans int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).
			Scan(&plans),
	)
	require.Zero(t, plans, "crash happened before either final plan or terminal marker")
	var query string
	switch mode {
	case "missing_generation":
		query = `UPDATE bot.interactions SET content=(SELECT jsonb_agg(r-'history_generation') FROM jsonb_array_elements(content) r)
 WHERE update_id=1 AND kind='script_runs'`
	case "redacted":
		query = `UPDATE bot.interactions SET content=(SELECT jsonb_agg(jsonb_build_object('history_generation',1,'history_redacted',true)) FROM jsonb_array_elements(content))
 WHERE update_id=1 AND kind='script_runs'`
	default:
		return
	}
	_, err := f.db.Exec(t.Context(), query)
	require.NoError(t, err)
}

func assertInterruptedMarkerRetry(t *testing.T, f *fixture, calls *atomic.Int64) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `CREATE SEQUENCE bot.marker_attempts;
 CREATE FUNCTION bot.reject_terminal_marker() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN PERFORM nextval('bot.marker_attempts'); RAISE EXCEPTION 'synthetic marker persistence failure'; END $$;
 CREATE TRIGGER reject_terminal_marker BEFORE INSERT ON interaction.saved_turns FOR EACH ROW EXECUTE FUNCTION bot.reject_terminal_marker()`)
	require.NoError(t, err)
	for range 2 {
		restarted := *f.b
		f.b = &restarted
		runInboxDatabaseFailure(t, f)
	}
	var attempts int
	var called bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT last_value,is_called FROM bot.marker_attempts`).Scan(&attempts, &called),
	)
	assert.True(t, called)
	assert.Equal(t, 2, attempts, "each SQL failure stops its runtime before another attempt")
	assert.Zero(t, calls.Load(), "fresh runtimes must retry persistence without model regeneration")
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	assert.Equal(t, 2, pending, "no acknowledgement before a durable terminal marker")
	var terminalRows, failures int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).
			Scan(&terminalRows),
	)
	assert.Zero(t, terminalRows, "failed terminal writes cannot acknowledge a stale update")
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT sum(failures) FROM bot.telegram_inbox`).Scan(&failures))
	assert.Zero(t, failures, "SQL failures do not consume the poison-update budget")
	var processed int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT value FROM bot.cursors WHERE name='telegram'`).Scan(&processed),
	)
	assert.Zero(t, processed, "failed terminal persistence cannot advance completion")
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_terminal_marker ON interaction.saved_turns`)
	require.NoError(t, err)
}
