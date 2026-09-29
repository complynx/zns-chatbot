package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type cancelledKnowledgeAccounting struct {
	cancel           context.CancelFunc
	cleanups         int
	cleanupCancelled bool
}

func (*cancelledKnowledgeAccounting) RequestTier() string { return "" }
func (r *cancelledKnowledgeAccounting) Reserve(context.Context, credits.Attempt) error {
	r.cancel()
	return nil
}
func (*cancelledKnowledgeAccounting) Dispatch(context.Context, string) error { return nil }
func (*cancelledKnowledgeAccounting) Settle(context.Context, string, credits.Settlement) error {
	return errors.New("unexpected settlement")
}
func (r *cancelledKnowledgeAccounting) NotSent(ctx context.Context, _ string) error {
	r.cleanups++
	r.cleanupCancelled = ctx.Err() != nil
	return errors.New("private recorder diagnostic")
}

func TestKnowledgeCoordinatorPreservesCancelledAccounting(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"execute", "retry"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			testKnowledgeCoordinatorCancelledAccounting(t, path)
		})
	}
}

func testKnowledgeCoordinatorCancelledAccounting(t *testing.T, path string) {
	t.Helper()
	f := knowledgeAuthorizationFixture(t)
	const updateID int64 = 88793
	require.NoError(
		t,
		f.b.Host.ArchiveOriginal(
			t.Context(),
			"bob",
			"tg-user-88793",
			"user",
			"Synthetic accounting cancellation request",
		),
	)
	coordinator := interaction.KnowledgeCoordinator{
		Client: f.b.API, Host: f.b.Host, Store: interaction.Store{DB: f.db},
	}
	command := knowledge.Command{
		Name: knowledge.Suggest, Topic: "travel", FactKey: "recovery",
		Text: "Synthetic pending proposal for cancellation accounting proof.",
	}
	_, err := coordinator.Execute(t.Context(), "bob", updateID, command, nil)
	require.NoError(t, err)
	var proposal knowledge.Proposal
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT id, version FROM core.knowledge_proposals WHERE owner='bob' AND fact_key='recovery'`).
		Scan(&proposal.ID, &proposal.Version))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	recorder := &cancelledKnowledgeAccounting{cancel: cancel}
	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	coordinator.Assessor = agenthost.KnowledgeAssessor{
		Authority:  f.b.Host,
		Classifier: agent.OpenAI{Key: "synthetic", BaseURL: server.URL, HTTP: server.Client(), Accounting: recorder},
	}
	if path == "retry" {
		_, err = coordinator.RetryAssessment(ctx, "bob", updateID, knowledge.Command{
			ProposalID: proposal.ID, Version: proposal.Version,
		})
	} else {
		_, err = coordinator.Execute(ctx, "bob", updateID, command, nil)
	}
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, sent.Load())
	require.Equal(t, 1, recorder.cleanups)
	require.False(t, recorder.cleanupCancelled)
	require.NotContains(t, err.Error(), "private recorder diagnostic")
	assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
	var winners int
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		`SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='knowledge_assessment'`,
		updateID,
	).
		Scan(&winners))
	require.Zero(t, winners)
	require.ErrorIs(t, err, credits.ErrAccounting)
}
