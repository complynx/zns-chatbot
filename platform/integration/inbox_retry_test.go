package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestInboxPoisonUpdateRetainsChatOrderWhileOtherChatProgresses(t *testing.T) {
	t.Parallel()
	f := setup(t)
	const poison = "Read permanently unavailable order"
	const following = "Alice must wait for her earlier update"
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": identity.AliceTelegramID, "text": poison})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": identity.AliceTelegramID, "text": following})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": identity.BobTelegramID, "text": "/start"})
	batch, err := f.b.TG.Updates(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	require.Equal(t, []int64{1, 2, 3}, []int64{batch[0].ID, batch[1].ID, batch[2].ID})

	firstAttempt := make(chan struct{})
	releaseFailure := make(chan struct{})
	var poisonCalls, followingCalls atomic.Int64
	f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
		if input.Text != poison {
			followingCalls.Add(1)
			return agent.Plan{View: "workflow", Text: "Later Alice update overtook poison"}, nil
		}
		if poisonCalls.Add(1) == 1 {
			close(firstAttempt)
			select {
			case <-releaseFailure:
			case <-ctx.Done():
				return agent.Plan{}, ctx.Err()
			}
		}
		// This read failure propagates through the handler instead of becoming
		// a successful generic model-unavailable notice. It has no SQL marker.
		return agent.Plan{}, interaction.ErrOrderReadUnavailable
	})
	stop := startRetryInbox(t, f)
	select {
	case <-firstAttempt:
	case <-time.After(5 * time.Second):
		t.Fatal("poison update did not reach its first handler attempt")
	}
	require.Equal(t, batch, retryInboxPayloads(t, f), "the entire received batch is durable before handling")
	var received, processed int64
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT value FROM bot.cursors WHERE name='telegram_received'").Scan(&received))
	require.Equal(t, int64(4), received)
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT value FROM bot.cursors WHERE name='telegram'").Scan(&processed))
	require.Zero(t, processed, "a blocked handler has not completed any update")
	close(releaseFailure)
	assert.Eventually(t, func() bool {
		for _, visible := range chatMessages(t, f, identity.BobTelegramID) {
			if visible.From.IsBot && visible.Text != "" {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "Bob must receive a real reply while Alice's first update fails")
	stop()
	require.Positive(t, poisonCalls.Load())
	require.Zero(t, followingCalls.Load(), "Alice's later update must not overtake her retained poison update")
	require.Equal(t, batch[:2], retryInboxPayloads(t, f), "both Alice payloads remain intact; Bob completed")
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT value FROM bot.cursors WHERE name='telegram_received'").Scan(&received))
	require.Equal(t, int64(4), received, "Telegram acknowledgement follows durable receipt, not successful handling")
	upstream, err := f.b.TG.Updates(t.Context(), received)
	require.NoError(t, err)
	require.Empty(t, upstream)
	require.Equal(t, batch[:2], retryInboxPayloads(t, f), "upstream acknowledgement cannot discard retained work")
}

func retryInboxPayloads(t *testing.T, f *fixture) []telegram.Update {
	t.Helper()
	rows, err := f.db.Query(t.Context(), "SELECT payload FROM bot.telegram_inbox ORDER BY update_id")
	require.NoError(t, err)
	defer rows.Close()
	var updates []telegram.Update
	for rows.Next() {
		var payload []byte
		require.NoError(t, rows.Scan(&payload))
		var update telegram.Update
		require.NoError(t, json.Unmarshal(payload, &update))
		updates = append(updates, update)
	}
	require.NoError(t, rows.Err())
	return updates
}

func TestInboxFailureCooldownSurvivesRestartAndPreservesFIFO(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": identity.AliceTelegramID, "text": "first retry"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": identity.AliceTelegramID, "text": "second retry"})
	deadline := persistInboxFailure(t, f)
	pending := retryInboxPayloads(t, f)
	require.Len(t, pending, 2)
	upstream, err := f.b.TG.Updates(t.Context(), 3)
	require.NoError(t, err)
	require.Empty(t, upstream, "restart must recover already acknowledged updates from durable storage")
	type invocation struct {
		text string
		at   time.Time
	}
	seen := make(chan invocation, 4)
	restarted := *f.b
	restarted.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
		var now time.Time
		if queryErr := f.db.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); queryErr != nil {
			return agent.Plan{}, core.DatabaseOperationError(queryErr)
		}
		select {
		case seen <- invocation{text: input.Text, at: now}:
		case <-ctx.Done():
			return agent.Plan{}, ctx.Err()
		}
		return agent.Plan{View: "workflow", Text: "Recovered " + input.Text}, nil
	})
	f.b = &restarted
	stop := startRetryInbox(t, f)
	require.Eventually(t, func() bool {
		var count int
		queryErr := f.db.QueryRow(t.Context(), "SELECT count(*) FROM bot.telegram_inbox").Scan(&count)
		return queryErr == nil && count == 0 && len(seen) >= 2
	}, time.Until(deadline)+10*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		for _, visible := range chatMessages(t, f, identity.AliceTelegramID) {
			if visible.From.IsBot && strings.Contains(visible.Text, "Recovered second retry") {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "reconstructed runtime must deliver the recovered answer")
	stop()
	require.Len(t, seen, 2, "each retained update is handled once after reconstruction")
	first, second := <-seen, <-seen
	require.Equal(t, "first retry", first.text)
	require.Equal(t, "second retry", second.text)
	require.False(t, first.at.Before(deadline), "restart must honor the persisted cooldown before calling the model")
	require.Empty(t, retryInboxPayloads(t, f))
}

func TestInboxSQLFailureDoesNotConsumeRetryBudget(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": identity.AliceTelegramID, "text": "recover after SQL"})
	deadline := persistInboxFailure(t, f)
	pending := retryInboxPayloads(t, f)
	require.Len(t, pending, 1)
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION bot.reject_retry_input() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.owner='alice' AND NEW.kind='input' THEN
 RAISE EXCEPTION 'private retry input SQL diagnostic'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_retry_input BEFORE INSERT ON bot.interactions
FOR EACH ROW EXECUTE FUNCTION bot.reject_retry_input()`)
	require.NoError(t, err)
	var calls atomic.Int64
	restarted := *f.b
	restarted.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		calls.Add(1)
		return agent.Plan{View: "workflow", Text: "Recovered after SQL"}, nil
	})
	f.b = &restarted
	ctx, cancel := context.WithTimeout(t.Context(), time.Until(deadline)+10*time.Second)
	defer cancel()
	runErr := f.b.Run(ctx)
	require.ErrorIs(t, runErr, core.ErrDatabase)
	require.NotContains(t, runErr.Error(), "private retry input SQL diagnostic")
	require.NoError(t, ctx.Err(), "SQL failure must stop the runtime before its timeout")
	var failures int
	var state string
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT failures,state FROM bot.telegram_inbox WHERE update_id=1").Scan(&failures, &state))
	require.Equal(t, 1, failures, "only the earlier non-SQL failure consumes retry budget")
	require.Equal(t, "pending", state)
	require.Equal(t, pending, retryInboxPayloads(t, f))
	require.Equal(t, int64(1), calls.Load())
	_, err = f.db.Exec(t.Context(), "DROP TRIGGER reject_retry_input ON bot.interactions")
	require.NoError(t, err)
	recovered := *f.b
	f.b = &recovered
	stop := startRetryInbox(t, f)
	require.Eventually(t, func() bool {
		var count int
		queryErr := f.db.QueryRow(t.Context(), "SELECT count(*) FROM bot.telegram_inbox").Scan(&count)
		return queryErr == nil && count == 0
	}, 15*time.Second, 20*time.Millisecond)
	stop()
	require.Equal(t, int64(1), calls.Load(), "SQL recovery reuses the saved winner")
}

func persistInboxFailure(t *testing.T, f *fixture) time.Time {
	t.Helper()
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		return agent.Plan{}, interaction.ErrOrderReadUnavailable
	})
	stop := startRetryInbox(t, f)
	require.Eventually(t, func() bool {
		var failures int
		queryErr := f.db.QueryRow(t.Context(),
			"SELECT failures FROM bot.telegram_inbox WHERE update_id=1").Scan(&failures)
		return queryErr == nil && failures == 1
	}, 5*time.Second, 10*time.Millisecond)
	stop()
	var deadline time.Time
	var failures int
	var state string
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT failures,state,next_attempt_at FROM bot.telegram_inbox WHERE update_id=1").
		Scan(&failures, &state, &deadline))
	require.Equal(t, 1, failures)
	require.Equal(t, "pending", state)
	require.True(t, deadline.After(time.Now()), "the failed update has a real future retry deadline")
	require.Less(t, time.Until(deadline), 15*time.Second, "first retry must fit the bounded runtime proof")
	return deadline
}

func startRetryInbox(t *testing.T, f *fixture) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			cancel()
			select {
			case runErr := <-done:
				if runErr != nil {
					require.ErrorIs(t, runErr, context.Canceled)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("inbox runtime did not stop after cancellation")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}
