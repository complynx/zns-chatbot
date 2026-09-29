package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
)

func TestAgentHostReadReservationsSurviveRestart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	const updateID int64 = 94001
	store := agenthost.ReadStore{DB: f.db}
	first := agent.KnowledgeProposal{Text: "first read"}
	index, err := store.ReserveKnowledge(t.Context(), "alice", updateID, first)
	require.NoError(t, err)
	require.Zero(t, index)

	// Reconstruct the host after an interrupted fetch. The durable slot stays
	// consumed; a new host instance cannot reset the update's budget.
	store = agenthost.ReadStore{DB: f.db}
	reads, err := store.Knowledge(t.Context(), "alice", updateID)
	require.NoError(t, err)
	require.Len(t, reads, 1)
	require.Equal(t, first, reads[0].Request)
	require.Equal(t, "interrupted", reads[0].Error)
	second := agent.KnowledgeProposal{Text: "second read"}
	index, err = store.ReserveKnowledge(t.Context(), "alice", updateID, second)
	require.NoError(t, err)
	require.Equal(t, 1, index)
	_, err = store.ReserveKnowledge(t.Context(), "alice", updateID, second)
	require.ErrorContains(t, err, "budget exhausted")
	reads, err = store.CompleteKnowledge(t.Context(), "alice", updateID, index,
		agent.KnowledgeReadResult{Request: second, Error: "unavailable"})
	require.NoError(t, err)
	require.Len(t, reads, agent.MaxKnowledgeReads)
	require.Equal(t, "interrupted", reads[0].Error)
	require.Equal(t, "unavailable", reads[1].Error)

	registration := agent.RegistrationProposal{Event: "sandbox", View: "events"}
	_, err = store.ReserveRegistration(t.Context(), "alice", updateID, registration)
	require.NoError(t, err)
	store = agenthost.ReadStore{DB: f.db}
	_, err = store.ReserveRegistration(t.Context(), "alice", updateID, registration)
	require.ErrorContains(t, err, "already available")
	var persisted []agent.RegistrationReadResult
	err = f.db.QueryRow(t.Context(),
		`SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='registration_reads'`,
		"alice", updateID).Scan(&persisted)
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	require.Equal(t, "read_pending", persisted[0].Error)
}
