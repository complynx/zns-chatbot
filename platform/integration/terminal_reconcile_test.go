package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestTerminalNoticeSurvivesNormalReconciliation(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	latePlanModel(t, f, "direct", func(agent.Input) (agent.Plan, error) {
		removeArchivedBooking(t, f)
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	require.ErrorContains(
		t,
		f.b.Handle(t.Context(), message(47109, 101, "Read private registration")),
		"terminal registration plan",
	)
	pumpBotDeliveries(t, f.b)
	cards := chatMessages(t, f, 101)
	require.NotEmpty(t, cards)
	require.Contains(t, cards[len(cards)-1].Text, "no longer available")
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	cards = chatMessages(t, f, 101)
	require.Contains(
		t,
		cards[len(cards)-1].Text,
		"no longer available",
		"periodic reconciliation must preserve the latest safe refusal",
	)
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		return agent.Plan{View: "workflow", Text: "A new independent answer."}, nil
	})
	handleVisible(t, f.b, message(47110, 101, "Start a new request"))
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	cards = chatMessages(t, f, 101)
	require.Contains(t, cards[len(cards)-1].Text, "A new independent answer.")
	require.ErrorContains(
		t,
		f.b.Handle(t.Context(), message(47109, 101, "Read private registration")),
		"terminal registration plan",
	)
	pumpBotDeliveries(t, f.b)
	require.Equal(t, cards, chatMessages(t, f, 101), "an old terminal update must not replace a newer answer")
}
