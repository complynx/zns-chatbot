package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func memorySplitRoleFixture(t *testing.T) *fixture {
	t.Helper()
	return restrictMemoryBotRole(t, setup(t))
}

func restrictMemoryBotRole(t *testing.T, f *fixture) *fixture {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `GRANT USAGE ON SCHEMA bot TO zns_bot;
 GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA bot TO zns_bot;
 GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA bot TO zns_bot`)
	require.NoError(t, err)
	cfg := f.db.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, roleErr := conn.Exec(ctx, "SET ROLE zns_bot")
		return roleErr
	}
	restricted, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(restricted.Close)
	for _, query := range []string{`SELECT body FROM core.knowledge_memos LIMIT 1`, `SELECT text FROM core.conversation_events LIMIT 1`} {
		_, err = restricted.Exec(t.Context(), query)
		var denied *pgconn.PgError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "42501", denied.Code)
	}
	f.b.DB = restricted
	return f
}

func TestMemoryOrdinaryProvenanceWithSplitBotRole(t *testing.T) {
	t.Parallel()
	for _, name := range []string{knowledge.MemoSet, knowledge.Curate, knowledge.Suggest} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := memorySplitRoleFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','curate'),('','bob','review')`,
			)
			require.NoError(t, err)
			action := &agent.KnowledgeProposal{
				Name:    name,
				Topic:   "travel",
				FactKey: "train",
				Text:    "Train starts at noon",
			}
			plans := []agent.Plan{{View: agent.KnowledgeView, KnowledgeAction: action}}
			if name == knowledge.MemoSet {
				action.Topic = ""
			}
			if name == knowledge.Curate {
				plans = append(
					[]agent.Plan{
						{
							View: agent.KnowledgeView,
							KnowledgeAction: &agent.KnowledgeProposal{
								Name:    agent.KnowledgeRead,
								Topic:   "travel",
								FactKey: "train",
							},
						},
					},
					plans...)
			}
			f.b.Model = &knowledgeModel{plans: plans}
			update := message(17001, identity.AliceTelegramID, "Remember the train starts at noon")
			handle(t, f.b, update)
			handle(t, f.b, update)
			if name == knowledge.Suggest {
				proposals, listErr := f.b.API.KnowledgeProposals(t.Context(), "alice", knowledge.ProposalQuery{})
				require.NoError(t, listErr)
				require.Len(t, proposals, 1)
				require.Equal(t, "pending_review", proposals[0].State)
				_, err = f.b.API.ExecuteKnowledge(
					t.Context(),
					"bob",
					knowledge.Command{
						Name:       knowledge.Review,
						Key:        "approve-ordinary",
						ProposalID: proposals[0].ID,
						Version:    proposals[0].Version,
						Decision:   "approve",
					},
				)
				require.NoError(t, err)
			}
			assertMemorySourceForOwner(t, f, "Remember the train starts at noon")
		})
	}
}

func TestMemoryScriptProvenanceWithSplitBotRole(t *testing.T) {
	t.Parallel()
	f := memorySplitRoleFixture(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			scriptCall(
				ctx,
				t,
				callback,
				"memory.write",
				`{"name":"document_set","topic":"travel","key":"train","text":"Train starts at noon"}`,
			)
			return json.RawMessage(`{"saved":true}`), nil
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Saved"},
	}}
	update := message(17002, identity.AliceTelegramID, "Remember the train starts at noon")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assertMemorySourceForOwner(t, f, "Remember the train starts at noon")
}

func assertMemorySourceForOwner(t *testing.T, f *fixture, want string) {
	t.Helper()
	service := knowledge.Service{DB: f.db}
	page, err := service.SearchMemory(t.Context(), "alice", knowledge.MemoryQuery{})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	sources, err := service.MemorySources(t.Context(), "alice", page.Entries[0].Ref)
	require.NoError(t, err)
	require.Len(t, sources.Events, 1)
	assert.Equal(t, want, sources.Events[0].Text)
	other, err := service.MemorySources(t.Context(), "bob", page.Entries[0].Ref)
	require.NoError(t, err)
	assert.Empty(t, other.Events)
}

type memoryRetryClassifier struct {
	*knowledgeModel

	attempts int
}

func (m *memoryRetryClassifier) AssessKnowledge(
	context.Context,
	agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	m.attempts++
	if m.attempts == 1 {
		return agent.KnowledgeAssessment{}, errors.New("temporary classifier failure")
	}
	return agent.KnowledgeAssessment{Worthwhile: true, Reason: "Useful fact"}, nil
}

func TestMemorySuggestionRetryAndManualApprovalKeepSource(t *testing.T) {
	t.Parallel()
	f := memorySplitRoleFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	f.b.Model = &memoryRetryClassifier{
		knowledgeModel: &knowledgeModel{
			plans: []agent.Plan{
				{
					View: agent.KnowledgeView,
					KnowledgeAction: &agent.KnowledgeProposal{
						Name:    knowledge.Suggest,
						Topic:   "travel",
						FactKey: "train",
						Text:    "Train starts at noon",
					},
				},
			},
		},
	}
	handle(t, f.b, message(17003, identity.AliceTelegramID, "Remember the train starts at noon"))
	var retry string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind FROM bot.interactions WHERE owner='alice' AND kind LIKE 'knowledge:%' AND content->'command'->>'name'='retry_assessment' LIMIT 1`).
			Scan(&retry),
	)
	handle(t, f.b, aliceCallback(17004, 1, retry))
	handle(t, f.b, message(17005, identity.BobTelegramID, "/knowledge"))
	var navigation string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind FROM bot.interactions WHERE owner='bob' AND kind LIKE 'knowledge:%' AND content->'view'->>'mode'='review' LIMIT 1`).
			Scan(&navigation),
	)
	callback := aliceCallback(17006, 1, navigation)
	callback.Callback.From.ID = identity.BobTelegramID
	callback.Callback.Message.Chat.ID = identity.BobTelegramID
	handle(t, f.b, callback)
	var approve string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind FROM bot.interactions WHERE owner='bob' AND kind LIKE 'knowledge:%' AND content->'command'->>'decision'='approve' LIMIT 1`).
			Scan(&approve),
	)
	callback.ID = 17007
	callback.Callback.Data = approve
	handle(t, f.b, callback)
	handle(t, f.b, callback)
	assertMemorySourceForOwner(t, f, "Remember the train starts at noon")
}

func TestMemoryHistorySummaryWithSplitBotRole(t *testing.T) {
	t.Parallel()
	f := memorySplitRoleFixture(t)
	f.b.HistoryLimit = 2
	archive := conversation.Service{DB: f.db}
	for i := range 8 {
		require.NoError(t, archive.Append(t.Context(), "alice", "old-"+strconv.Itoa(i), "user", "Previous preferences"))
	}
	model := &historyModel{plans: []agent.Plan{{View: "workflow", Text: "Earlier preferences summarized"}}}
	f.b.Model = model
	update := message(17008, identity.AliceTelegramID, "What were my earlier preferences?")
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.Len(t, model.summaries, 1)
	require.Len(t, model.inputs, 1)
	require.NotNil(t, model.inputs[0].Conversation)
	assert.Contains(t, model.inputs[0].Conversation.Summary.Text, "no booking was confirmed")
}

func TestMemoryPassNotificationArchiveWithSplitBotRole(t *testing.T) {
	t.Parallel()
	f := restrictMemoryBotRole(t, registrationPaymentFixture(t))
	drainPassNotices(t, f)
	before := chatMessages(t, f, identity.AliceTelegramID)
	require.NotEmpty(t, before)
	drainPassNotices(t, f)
	assert.Equal(t, before, chatMessages(t, f, identity.AliceTelegramID))
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND source_key LIKE 'pass-notification-%'`).
			Scan(&count),
	)
	assert.Equal(t, len(before), count)
}
