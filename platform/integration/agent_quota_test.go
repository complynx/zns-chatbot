package integration_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestAssistantQuotaLocalesManualAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			f.b.AssistantDailyLimit = 1
			_, err := f.b.API.SetLanguage(t.Context(), "alice", locale, false)
			require.NoError(t, err)
			handle(t, f.b, message(9100, 101, "question"))
			require.NotNil(t, f.model.input.AssistantQuestionsRemaining)
			assert.Zero(t, *f.model.input.AssistantQuestionsRemaining)
			handle(t, f.b, message(9101, 101, "another question"))
			assert.Equal(t, 1, f.model.calls)
			text, err := i18n.Translate(locale, i18n.AgentQuotaReached, nil)
			require.NoError(t, err)
			var reply string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT payload->'plan'->>'text' FROM interaction.saved_turns WHERE owner='alice' AND update_id=9101`).
					Scan(&reply),
			)
			assert.Equal(t, text, reply)
			handle(t, f.b, message(9102, 101, "/start"))
			handle(t, f.b, aliceCallback(9103, f.aliceCard(t), "select:massage-1:0"))
			workflow, err := f.b.API.Current(t.Context(), "alice")
			require.NoError(t, err)
			assert.Equal(t, "draft", workflow.State)
			handle(t, f.b, message(9104, 202, "question"))
			assert.Equal(t, 2, f.model.calls)
		})
	}
}

func TestAssistantQuotaDefaultRollingWindowAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.agent_quota(owner,update_id,allowed,remaining)
	 SELECT 'alice',-n,true,0 FROM generate_series(1,49) n`)
	require.NoError(t, err)
	handle(t, f.b, message(9200, 101, "last question"))
	assert.Zero(t, *f.model.input.AssistantQuestionsRemaining)
	handle(t, f.b, message(9201, 101, "over limit"))
	assert.Equal(t, 1, f.model.calls)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.agent_quota SET reserved_at=clock_timestamp()-interval '24 hours 1 second' WHERE allowed`,
	)
	require.NoError(t, err)
	// A fresh process still replays the persisted rejection after the window moves.
	restarted := *f.b
	handle(t, &restarted, message(9201, 101, "over limit"))
	assert.Equal(t, 1, f.model.calls)
	handle(t, &restarted, message(9202, 101, "new window"))
	assert.Equal(t, 2, f.model.calls)
	assert.Equal(t, 49, *f.model.input.AssistantQuestionsRemaining)
}

type quotaModel struct {
	calls atomic.Int64
	fail  atomic.Bool
}

func (m *quotaModel) Plan(_ context.Context, _ agent.Input) (agent.Plan, error) {
	m.calls.Add(1)
	if m.fail.Load() {
		return agent.Plan{}, context.Canceled
	}
	return agent.Plan{Text: "answer", View: "workflow"}, nil
}

func TestAssistantQuotaInterruptedAttemptRetry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.AssistantDailyLimit = 1
	model := &quotaModel{}
	model.fail.Store(true)
	f.b.Model = model
	require.ErrorIs(t, f.b.Handle(t.Context(), message(9300, 101, "question")), context.Canceled)
	model.fail.Store(false)
	restarted := *f.b
	handle(t, &restarted, message(9300, 101, "question"))
	handle(t, &restarted, message(9301, 101, "next"))
	assert.EqualValues(t, 2, model.calls.Load())
	var charged int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.agent_quota WHERE allowed`).Scan(&charged))
	assert.Equal(t, 1, charged)
}

func TestAssistantQuotaConcurrentUpdates(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.AssistantDailyLimit = 3
	model := &quotaModel{}
	f.b.Model = model
	const attempts = 12
	results := make(chan error, attempts)
	for n := range attempts {
		go func() {
			results <- f.b.Handle(t.Context(), message(int64(9400+n), 101, "question"))
		}()
	}
	for range attempts {
		require.NoError(t, <-results)
	}
	assert.EqualValues(t, 3, model.calls.Load())
	var charged int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.agent_quota WHERE allowed`).Scan(&charged))
	assert.Equal(t, 3, charged)
}
