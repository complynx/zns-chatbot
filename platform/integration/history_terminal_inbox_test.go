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

// Exercise the durable poller boundary, including a failed acknowledgement and
// a stopped/restarted Run, rather than accepting an error from Handle as proof.
func TestHistoryTerminalPlanDoesNotPoisonInboxAfterRestart(t *testing.T) {
	t.Parallel()
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
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION bot.hold_terminal_ack() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF OLD.update_id=1 THEN RAISE EXCEPTION 'synthetic acknowledgement failure'; END IF; RETURN OLD; END $$;
 CREATE TRIGGER hold_terminal_ack BEFORE DELETE ON bot.telegram_inbox FOR EACH ROW EXECUTE FUNCTION bot.hold_terminal_ack()`)
	require.NoError(t, err)
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
	close(releaseFinal)
	require.Eventually(t, func() bool {
		var terminal bool
		queryErr := f.db.QueryRow(t.Context(), `SELECT COALESCE((kind='terminal' AND state='privacy_terminal' AND reason='history_deleted'),false) FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).
			Scan(&terminal)
		return queryErr == nil && terminal
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.EqualValues(t, 2, calls.Load())
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Equal(t, 2, pending, "failed acknowledgement must preserve both durable updates")
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER hold_terminal_ack ON bot.telegram_inbox`)
	require.NoError(t, err)
	// A fresh host instance must consume the saved terminal marker without a model
	// call or repeated effect, then process the following request normally.
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

type terminalSummaryModel struct {
	plan      func(context.Context, agent.Input) (agent.Plan, error)
	summarize func(context.Context, agent.HistorySummaryInput) (string, error)
}

func (m terminalSummaryModel) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	return m.plan(ctx, input)
}

func (m terminalSummaryModel) SummarizeHistory(ctx context.Context, input agent.HistorySummaryInput) (string, error) {
	return m.summarize(ctx, input)
}

func TestHistorySummaryDeletionDoesNotPoisonFollowingUpdate(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.HistoryLimit = 1
	archive := conversation.Service{DB: f.db}
	const canary = "stale summary source canary"
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "summary-source", "user", canary))
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "recent-source", "assistant", "recent safe event"))
	var eventID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='summary-source'`).
			Scan(&eventID),
	)
	var summaryCalls, planCalls atomic.Int64
	f.b.Model = terminalSummaryModel{
		summarize: func(ctx context.Context, input agent.HistorySummaryInput) (string, error) {
			if summaryCalls.Add(1) == 1 {
				raw, err := json.Marshal(input)
				if err != nil {
					return "", err
				}
				assert.Contains(t, string(raw), canary)
				// The remote request was dispatched before deletion; its eventual
				// response must lose CAS without making this inbox item permanent.
				if err = archive.DeleteContent(ctx, "alice", eventID); err != nil {
					return "", err
				}
				return canary, nil
			}
			return "safe summary", nil
		},
		plan: func(_ context.Context, input agent.Input) (agent.Plan, error) {
			planCalls.Add(1)
			raw, err := json.Marshal(input)
			if err != nil {
				return agent.Plan{}, err
			}
			assert.NotContains(t, string(raw), canary)
			return agent.Plan{View: "workflow", Text: "safe reply"}, nil
		},
	}
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "summarize earlier history"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "following summary request"})
	completeInbox(t, f, 3)
	assert.EqualValues(t, 2, planCalls.Load())
	var terminal bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE((kind='terminal' AND state='privacy_terminal' AND reason='history_deleted'),false) FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).
			Scan(&terminal),
	)
	assert.True(t, terminal)
	window, err := archive.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	assert.NotContains(t, window.Summary.Text, canary)
}
