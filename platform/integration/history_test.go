package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestHistoryOwnerAPIAndCommittedAudit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := conversation.Service{DB: f.db}
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "private", "user", "I prefer vegetarian meals"))
	require.NoError(t, service.AppendOriginal(t.Context(), "bob", "other", "user", "Bob private marker"))
	_, err := f.b.API.Execute(t.Context(), "alice", action("select", "massage-1", 0, "history-action", "manual"))
	require.NoError(t, err)
	page, err := f.b.API.ConversationHistory(t.Context(), "alice", conversation.Query{Limit: conversation.MaxPage})
	require.NoError(t, err)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "vegetarian")
	assert.Contains(t, string(encoded), `"action":"select"`)
	assert.NotContains(t, string(encoded), "Bob private marker")
	_, err = service.Read(t.Context(), "unknown", conversation.Query{Limit: 1})
	require.Error(t, err)
	require.NoError(
		t,
		service.AppendOriginal(t.Context(), "alice", "sensitive", "user", "My passport is SECRET-DOCUMENT"),
	)
	page, err = service.Read(t.Context(), "alice", conversation.Query{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.True(t, page.Events[0].Omitted)
	assert.NotContains(t, page.Events[0].Text, "SECRET-DOCUMENT")
}

func TestHistorySummaryCoverageIncludesLateLowerID(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := conversation.Service{DB: f.db}
	tx, err := f.db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var late int64
	require.NoError(
		t,
		tx.QueryRow(t.Context(), `INSERT INTO core.conversation_events(owner,source_key,kind,text) VALUES('alice','late','user','Late committed event') RETURNING id`).
			Scan(&late),
	)
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "visible", "user", "Visible earlier decision"))
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "latest", "assistant", "Recent answer"))
	window, err := service.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	assert.True(t, window.Gap)
	batch, err := service.SummaryBatch(t.Context(), "alice", window.BeforeID)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	require.NoError(
		t,
		service.CommitSummary(t.Context(), "alice", 0, []int64{batch[0].ID}, "An earlier decision was discussed."),
	)
	require.NoError(t, tx.Commit(t.Context()))
	window, err = service.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	assert.True(t, window.Gap)
	assert.Greater(t, window.Summary.ThroughID, late)
	batch, err = service.SummaryBatch(t.Context(), "alice", window.BeforeID)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, late, batch[0].ID)
	require.NoError(
		t,
		service.CommitSummary(
			t.Context(),
			"alice",
			1,
			[]int64{late},
			"The earlier decision and late message are summarized.",
		),
	)
	window, err = service.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	assert.False(t, window.Gap)
	err = service.CommitSummary(t.Context(), "bob", 0, []int64{late}, "Trying another owner's event")
	require.Error(t, err)
}

type historyModel struct {
	plans     []agent.Plan
	inputs    []agent.Input
	summaries []agent.HistorySummaryInput
	fail      bool
}

func (m *historyModel) Plan(_ context.Context, input agent.Input) (agent.Plan, error) {
	m.inputs = append(m.inputs, input)
	index := len(m.inputs) - 1
	if index >= len(m.plans) {
		index = len(m.plans) - 1
	}
	return m.plans[index], nil
}
func (m *historyModel) SummarizeHistory(_ context.Context, input agent.HistorySummaryInput) (string, error) {
	m.summaries = append(m.summaries, input)
	if m.fail {
		return "", errors.New("summary unavailable")
	}
	return "Earlier dietary preferences were discussed; no booking was confirmed.", nil
}

func TestHistoryBotRealSummaryAndBoundedReadReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.HistoryLimit = 2
	service := conversation.Service{DB: f.db}
	for index := range 8 {
		require.NoError(
			t,
			service.AppendOriginal(
				t.Context(),
				"alice",
				"seed-"+strconv.Itoa(index),
				"user",
				"Earlier dietary message "+strconv.Itoa(index),
			),
		)
	}
	model := &historyModel{
		plans: []agent.Plan{
			{View: "workflow", HistoryAction: &agent.HistoryProposal{}},
			{View: "workflow", Text: "Here is the earlier context."},
		},
	}
	f.b.Model = model
	update := message(900, identity.AliceTelegramID, "What did we discuss earlier?")
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.Len(t, model.inputs, 2)
	require.Len(t, model.summaries, 1)
	assert.Len(t, model.inputs[0].History, 2)
	require.NotNil(t, model.inputs[1].Conversation)
	assert.Contains(t, model.inputs[1].Conversation.Summary.Text, "no booking was confirmed")
	assert.False(t, model.inputs[1].Conversation.Gap)
	assert.Equal(t, 1, model.inputs[1].Conversation.Remaining)
	require.Len(t, model.inputs[1].Conversation.Reads, 1)
	require.NotEmpty(t, model.summaries[0].Events)
	assert.NotEqual(t, model.summaries[0].Events[0].Text, model.inputs[1].Conversation.Summary.Text)
}

func TestHistorySummaryFailureLeavesGapAndDoesNotBlockAnswer(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.HistoryLimit = 1
	service := conversation.Service{DB: f.db}
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "older", "user", "An earlier request"))
	require.NoError(t, service.AppendOriginal(t.Context(), "alice", "newer", "assistant", "A later answer"))
	model := &historyModel{plans: []agent.Plan{{View: "workflow", Text: "I can still answer."}}, fail: true}
	f.b.Model = model
	handle(t, f.b, message(910, identity.AliceTelegramID, "Hello"))
	require.Len(t, model.inputs, 1)
	assert.True(t, model.inputs[0].Conversation.Gap)
	assert.Empty(t, model.inputs[0].Conversation.Summary.Text)
}

func TestHistoryReadBudgetSurvivesInterruptedReservation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	reserved := []conversation.Page{{Error: "interrupted"}, {Error: "interrupted"}}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',920,'history_reads',$1)`,
		reserved,
	)
	require.NoError(t, err)
	model := &historyModel{plans: []agent.Plan{{View: "workflow", HistoryAction: &agent.HistoryProposal{}}}}
	f.b.Model = model
	update := message(920, identity.AliceTelegramID, "Read older messages")
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.Len(t, model.inputs, 1, "exhausted reads fail closed and the saved fallback replays without new model work")
	assert.Zero(t, model.inputs[0].Conversation.Remaining)
	require.Len(t, model.inputs[0].Conversation.Reads, agent.MaxHistoryReads)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT jsonb_array_length(content) FROM bot.interactions WHERE owner='alice' AND update_id=920 AND kind='history_reads'`).
			Scan(&count),
	)
	assert.Equal(t, agent.MaxHistoryReads, count)
}
