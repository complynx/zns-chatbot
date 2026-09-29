package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestHistoryReplyVisibilityAfterDeletionAndRestart(t *testing.T) {
	t.Parallel()
	for _, view := range []string{"workflow", agent.OrdersView, agent.ProfilesView, agent.KnowledgeView, agent.RegistrationView} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			archive := conversation.Service{DB: f.db}
			const canary = "orchid garden stale reply marker"
			require.NoError(t, archive.Append(t.Context(), "alice", "visibility-source", "user", canary))
			var eventID int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='visibility-source'`).
					Scan(&eventID),
			)
			f.model.plan = agent.Plan{View: view, Text: canary}
			update := message(70001, 101, "recall the previous detail")
			handle(t, f.b, update)
			assert.Contains(t, replyVisibilityState(t, f), canary)
			require.NoError(t, renderHistoryView(t.Context(), f.b, view))
			assert.Contains(t, replyVisibilityState(t, f), canary, "current saved generation remains renderable")
			require.NoError(t, archive.DeleteContent(t.Context(), "alice", eventID))
			restarted := *f.b
			f.b = &restarted
			require.NoError(t, renderHistoryView(t.Context(), f.b, view))
			assert.NotContains(t, replyVisibilityState(t, f), canary)
			var retained int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE update_id=70001 AND kind IN ('reply','orders_reply','profile_reply','profile_answer','knowledge_reply','registration_reply')`).
					Scan(&retained),
			)
			assert.Zero(t, retained)
			var terminal bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT (plan->>'history_redacted')::boolean FROM bot.replies WHERE update_id=70001`).
					Scan(&terminal),
			)
			assert.True(t, terminal)
			err := f.b.Handle(t.Context(), update)
			require.ErrorContains(t, err, "terminal history plan")
			require.NoError(t, renderHistoryView(t.Context(), f.b, view))
			assert.NotContains(t, replyVisibilityState(t, f), canary)
		})
	}
}

func renderHistoryView(ctx context.Context, b *bot.Bot, view string) error {
	switch view {
	case agent.OrdersView:
		return b.RenderOrders(ctx, "alice", 101)
	case agent.ProfilesView:
		return b.RenderProfile(ctx, "alice", 101)
	case agent.KnowledgeView:
		return b.RenderKnowledge(ctx, "alice", 101)
	case agent.RegistrationView:
		return b.RenderPassMenu(ctx, "alice", 101, "")
	default:
		return b.Render(ctx, "alice", 101)
	}
}

func replyVisibilityState(t *testing.T, f *fixture) string {
	t.Helper()
	raw, err := json.Marshal(chatMessages(t, f, 101))
	require.NoError(t, err)
	return string(raw)
}

func TestHistoryReplyVisibilityPreservesManualAndSystemNotices(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive := conversation.Service{DB: f.db}
	require.NoError(t, archive.Append(t.Context(), "alice", "manual-generation-source", "user", "old context"))
	var eventID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='manual-generation-source'`).
			Scan(&eventID),
	)
	require.NoError(t, archive.DeleteContent(t.Context(), "alice", eventID))
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',70002,'reply','"manual fixed notice"')`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	assert.Contains(t, replyVisibilityState(t, f), "manual fixed notice")
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO bot.replies(update_id,plan) VALUES(70003,'{"history_generation":0,"system_notice":"agent.quota_reached"}'); INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',70003,'reply','"fixed host notice"')`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	preference, err := f.b.API.Preferences(t.Context(), "alice")
	require.NoError(t, err)
	notice, err := i18n.Translate(preference.Language, i18n.AgentQuotaReached, nil)
	require.NoError(t, err)
	assert.Contains(t, replyVisibilityState(t, f), notice)
}
