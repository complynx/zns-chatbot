package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type knowledgeModel struct {
	plans       []agent.Plan
	inputs      []agent.Input
	assessments int
}

func (m *knowledgeModel) Plan(_ context.Context, input agent.Input) (agent.Plan, error) {
	m.inputs = append(m.inputs, input)
	index := len(m.inputs) - 1
	if index >= len(m.plans) {
		index = len(m.plans) - 1
	}
	return m.plans[index], nil
}

func (m *knowledgeModel) AssessKnowledge(
	context.Context,
	agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	m.assessments++
	return agent.KnowledgeAssessment{Worthwhile: true, Reason: "Useful concrete fact"}, nil
}

func TestKnowledgeBotMemoReadWriteReplayAndPrivacy(t *testing.T) {
	t.Parallel()
	f := setup(t)
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View:            agent.KnowledgeView,
			KnowledgeAction: &agent.KnowledgeProposal{Name: agent.KnowledgeMemoRead, FactKey: "food"},
		},
		{
			View:            agent.KnowledgeView,
			KnowledgeAction: &agent.KnowledgeProposal{Name: knowledge.MemoSet, FactKey: "food", Text: "vegetarian"},
		},
	}}
	f.b.Model = model
	update := message(800, identity.AliceTelegramID, "Remember that I prefer vegetarian food")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Len(t, model.inputs, 2)
	memo, err := f.b.API.Memo(t.Context(), "alice", "food")
	require.NoError(t, err)
	assert.Equal(t, "vegetarian", memo.Text)
	assert.EqualValues(t, 1, memo.Version)
	other, err := f.b.API.Memos(t.Context(), "bob")
	require.NoError(t, err)
	assert.Empty(t, other)
	var reads []agent.KnowledgeReadResult
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=800 AND kind='knowledge_reads'`).
			Scan(&reads),
	)
	require.Len(t, reads, 1)
	require.NotNil(t, reads[0].Memo)
	assert.Zero(t, reads[0].Memo.Version)
	model.plans = []agent.Plan{{View: "workflow", Text: "hello"}}
	handle(t, f.b, message(801, identity.AliceTelegramID, "Hello"))
	for _, event := range model.inputs[len(model.inputs)-1].History {
		assert.NotContains(t, event.Kind, "knowledge")
	}
}

func TestKnowledgeBotReadBudgetPersistsAndCannotBecomeMutation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	model := &knowledgeModel{
		plans: []agent.Plan{
			{View: agent.KnowledgeView, KnowledgeAction: &agent.KnowledgeProposal{Name: agent.KnowledgeRead}},
		},
	}
	f.b.Model = model
	update := message(820, identity.AliceTelegramID, "Read the facts")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Len(t, model.inputs, agent.MaxKnowledgeReads+1)
	var reads []agent.KnowledgeReadResult
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=820 AND kind='knowledge_reads'`).
			Scan(&reads),
	)
	assert.Len(t, reads, agent.MaxKnowledgeReads)
	var operations int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations`).Scan(&operations))
	assert.Zero(t, operations)
}

func TestKnowledgeBotSuggestionCannotPublishAndReviewButtonsAreOwnerBound(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	model := &knowledgeModel{
		plans: []agent.Plan{
			{
				View: agent.KnowledgeView,
				KnowledgeAction: &agent.KnowledgeProposal{
					Name:    knowledge.Suggest,
					Topic:   "travel",
					FactKey: "venue",
					Text:    "Venue opens at noon",
				},
			},
		},
	}
	f.b.Model = model
	update := message(810, identity.AliceTelegramID, "Suggest this venue fact")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Equal(t, 1, model.assessments)
	proposals, err := f.b.API.KnowledgeProposals(t.Context(), "alice", knowledge.ProposalQuery{})
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	assert.Equal(t, knowledge.AwaitingSubmission, proposals[0].State)
	submitKnowledgeCardForAlice(t, f, proposals[0], 81001)
	facts, err := f.b.API.Knowledge(t.Context(), "alice", knowledge.Query{})
	require.NoError(t, err)
	assert.Empty(t, facts)
	// The review navigation itself is owner-bound, before the domain checks rights.
	handle(t, f.b, message(811, identity.BobTelegramID, "/knowledge"))
	var navigation string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind FROM bot.interactions WHERE owner='bob' AND kind LIKE 'knowledge:%' AND content->'view'->>'mode'='review' LIMIT 1`).
			Scan(&navigation),
	)
	handle(t, f.b, aliceCallback(812, 1, navigation))
	var hasReview bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner='alice' AND kind='knowledge_view' AND content->>'mode'='review')`).
			Scan(&hasReview),
	)
	assert.False(t, hasReview)
	callback := aliceCallback(813, 1, navigation)
	callback.Callback.From.ID = identity.BobTelegramID
	callback.Callback.Message.Chat.ID = identity.BobTelegramID
	handle(t, f.b, callback)
	var approve string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind FROM bot.interactions WHERE owner='bob' AND kind LIKE 'knowledge:%' AND content->'command'->>'decision'='approve' LIMIT 1`).
			Scan(&approve),
	)
	callback.ID = 814
	callback.Callback.Data = approve
	handle(t, f.b, callback)
	handle(t, f.b, callback)
	facts, err = f.b.API.Knowledge(t.Context(), "alice", knowledge.Query{})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.EqualValues(t, 1, facts[0].Version)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	callback.ID = 815
	handle(t, f.b, callback)
	var notice string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content#>>'{}' FROM bot.interactions WHERE owner='bob' AND update_id=815 AND kind='knowledge_reply'`).
			Scan(&notice),
	)
	assert.Contains(t, notice, "недоступно")
}

// Exercise the author button through the bot transport, including foreign and replayed callbacks.
func submitKnowledgeCardForAlice(
	t *testing.T,
	f *fixture,
	proposal knowledge.Proposal,
	updateID int64,
) knowledge.Proposal {
	t.Helper()
	require.Equal(t, knowledge.AwaitingSubmission, proposal.State)
	queue, err := f.b.API.KnowledgeProposals(
		t.Context(),
		"bob",
		knowledge.ProposalQuery{Event: proposal.Event, ReviewQueue: true},
	)
	require.NoError(t, err)
	require.Empty(t, queue, "review grant alone cannot expose the private draft")
	var token string
	var submission knowledge.Submission
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT kind,content->'submission' FROM bot.interactions
 WHERE owner='alice' AND kind LIKE 'knowledge:%' AND (content->'submission'->>'proposal_id')::bigint=$1`, proposal.ID).Scan(&token, &submission))
	require.Equal(t, knowledge.Submission{ProposalID: proposal.ID, Version: proposal.Version, Event: proposal.Event,
		Topic: proposal.Topic, FactKey: proposal.FactKey, Text: proposal.Text}, submission)
	foreign := aliceCallback(updateID, 1, token)
	foreign.Callback.From.ID = identity.BobTelegramID
	foreign.Callback.Message.Chat.ID = identity.BobTelegramID
	handle(t, f.b, foreign)
	queue, err = f.b.API.KnowledgeProposals(
		t.Context(),
		"bob",
		knowledge.ProposalQuery{Event: proposal.Event, ReviewQueue: true},
	)
	require.NoError(t, err)
	require.Empty(t, queue, "another actor cannot use the author's consent button")
	callback := aliceCallback(updateID+1, 1, token)
	handle(t, f.b, callback)
	handle(t, f.b, callback)
	callback.ID = updateID + 2
	handle(t, f.b, callback)
	queue, err = f.b.API.KnowledgeProposals(
		t.Context(),
		"bob",
		knowledge.ProposalQuery{Event: proposal.Event, ReviewQueue: true},
	)
	require.NoError(t, err)
	require.Len(t, queue, 1)
	require.Equal(t, proposal.ID, queue[0].ID)
	require.Equal(t, proposal.Text, queue[0].Text)
	require.Equal(t, "pending_review", queue[0].State)
	require.True(t, queue[0].Submitted)
	require.Empty(t, queue[0].Reason, "classifier explanation is outside the consented body")
	return queue[0]
}
