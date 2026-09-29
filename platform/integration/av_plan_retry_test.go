package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestAVModelDeadlinePreservesEvidenceForRetry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	worker := &avWorker{result: avResult(t, false)}
	f.b.AV = worker
	update := avUpload(t, f, "voice")
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		return agent.Plan{}, context.DeadlineExceeded
	})
	// A provider timeout can expire while the parent event context remains alive.
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.DeadlineExceeded)
	var retained, replied bool
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT private_result IS NOT NULL FROM bot.av_results WHERE intake_id='tg-media-100'`).Scan(&retained))
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT EXISTS(SELECT 1 FROM interaction.saved_turns WHERE owner='alice' AND update_id=100)`).Scan(&replied))
	assert.True(t, retained)
	assert.False(t, replied)
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		require.NotNil(t, input.AV)
		assert.Equal(t, worker.result.Transcript.Text, input.AV.Transcript.Text)
		return agent.Plan{View: agent.OrdersView}, nil
	})
	handle(t, f.b, update)
	assert.Equal(t, 1, worker.initial, "retry must reuse the successful transcript")
}
