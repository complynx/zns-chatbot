package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestCodeQARevokedReplyDoesNotReturnThroughHistory(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	latePlanModel(t, f, "direct", func(agent.Input) (agent.Plan, error) {
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	handleVisible(t, f.b, message(47001, 101, "Read private registration"))
	before := workflowCard(t, f)
	require.Contains(t, before.Text, latePlanSecret, "authorized private reply must first be actually delivered")
	removeArchivedBooking(t, f)
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	after := workflowCard(t, f)
	require.Equal(t, before.ID, after.ID, "revocation must update the existing card")
	require.NotContains(t, after.Text, latePlanSecret)
	historyChecked := false
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		historyChecked = true
		data, err := json.Marshal(input.History)
		require.NoError(t, err)
		require.NotContains(
			t,
			string(data),
			latePlanSecret,
			"revoked derived output must not return through conversation context",
		)
		return agent.Plan{View: "workflow", Text: "ok"}, nil
	})
	handleVisible(t, f.b, message(47002, 101, "Next ordinary question"))
	require.True(t, historyChecked, "the next provider call must inspect the reauthorized history")
	require.NotContains(t, workflowCard(t, f).Text, latePlanSecret)
}

func TestCodeQABeforeProviderAuthorityOutageRemainsRetryable(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	transport := &passToolAuthorityTransport{}
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	calls := latePlanModel(t, f, "direct", func(input agent.Input) (agent.Plan, error) {
		transport.fail.Store(true)
		err := input.BeforeProvider(t.Context(), &input)
		require.Error(t, err)
		transport.fail.Store(false)
		return agent.Plan{}, err
	})
	update := message(47003, 101, "Read private registration")
	err := f.b.Handle(t.Context(), update)
	require.Error(t, err, "temporary authority failure must leave update retryable")
	var plans int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=47003`).
			Scan(&plans),
	)
	assert.Zero(t, plans, "outage must not become a saved fallback reply")
	require.Equal(t, 2, *calls)
}

type qaArchiveBoundaryTransport func(*http.Request) (*http.Response, error)

func (f qaArchiveBoundaryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCodeQAAuthorityLossAtArchiveBoundary(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	latePlanModel(
		t,
		f,
		"direct",
		func(agent.Input) (agent.Plan, error) { return agent.Plan{View: "workflow", Text: latePlanSecret}, nil },
	)
	revoked := false
	f.b.API.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/internal/history/archive/derived" {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if strings.Contains(string(body), "tg-assistant-47004") && !revoked {
				removeArchivedBooking(t, f)
				revoked = true
			}
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	f.b.Host.HTTP = f.b.API.HTTP
	err := f.b.Handle(t.Context(), message(47004, 101, "Read private registration"))
	require.True(t, revoked)
	require.ErrorContains(t, err, "terminal registration plan")
	var archived int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND source_key='tg-assistant-47004' AND text=$1`, latePlanSecret).
			Scan(&archived),
	)
	require.Zero(t, archived, "authority loss before archive request must prevent durable stale output")
}
