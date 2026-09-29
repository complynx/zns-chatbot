package integration_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type knowledgeAttachmentFailure struct {
	attachments atomic.Int64
	effects     atomic.Int64
}

func (f *knowledgeAttachmentFailure) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		switch request.URL.Path {
		case "/internal/memory/sources":
			if f.attachments.Add(1) <= 2 {
				return nil, errors.New("original source attachment interrupted")
			}
		case "/internal/knowledge/derived":
			f.effects.Add(1)
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}
func TestSavedKnowledgeReceiptCompletesOriginalSources(t *testing.T) {
	t.Parallel()
	for _, name := range []string{knowledge.MemoSet, knowledge.Curate} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, scenario := range []string{"retained", "source_revoked", "permission_revoked", "history_deleted"} {
				t.Run(scenario, func(t *testing.T) {
					t.Parallel()
					testSavedKnowledgeAttachment(t, name, scenario)
				})
			}
		})
	}
}
func testSavedKnowledgeAttachment(t *testing.T, name, scenario string) {
	t.Helper()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	const updateID int64 = 88931
	command := knowledge.Command{Name: name, FactKey: "recovery", Text: "Synthetic recovered knowledge"}
	if name == knowledge.Curate {
		command.Topic = "travel"
		command.Key = "preserved-knowledge-operation"
	}
	plan := interaction.SavedPlan{
		Plan:             agent.Plan{View: agent.KnowledgeView},
		KnowledgeCommand: &command,
		PassAuthority: &interaction.PlanAuthority{
			Reads: []interaction.PassContextDependency{},
			ReadAuthorities: []readsource.Authority{
				{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
			},
		},
	}
	plan.BindKind()
	_, err := (interaction.Store{DB: f.db}).SaveWinner(ctx, "bob", updateID, plan)
	require.NoError(t, err)
	require.NoError(t, f.b.Host.ArchiveOriginal(ctx, "alice", "tg-user-88931", "user", "Foreign request"))
	transport := &knowledgeAttachmentFailure{}
	f.b.Host.HTTP = &http.Client{Transport: transport}
	f.b.Model = &knowledgeModel{}
	update := message(updateID, identity.BobTelegramID, "Remember this original request")
	require.EqualError(t, f.b.Handle(ctx, update), "core API unavailable")
	require.EqualValues(t, 1, transport.effects.Load())
	require.EqualValues(t, 1, transport.attachments.Load())
	assertSavedKnowledgeLinks(t, f, 0)
	assertSavedKnowledgeReady(t, f, updateID)
	var deletedOriginal int64
	if scenario == "history_deleted" {
		require.NoError(
			t,
			f.db.QueryRow(ctx, "SELECT id FROM core.conversation_events WHERE owner='bob' AND source_key='tg-user-88931'").
				Scan(&deletedOriginal),
		)
		require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(ctx, "bob", deletedOriginal))
	} else if scenario != "retained" {
		permission := "review"
		if scenario == "permission_revoked" {
			permission = "curate"
		}
		_, err = f.db.Exec(
			ctx,
			"DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission=$1",
			permission,
		)
		require.NoError(t, err)
	}
	model := &knowledgeModel{}
	f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: model}
	switch {
	case scenario == "source_revoked" || scenario == "history_deleted":
		require.NoError(t, f.b.Handle(ctx, update))
		require.EqualValues(t, 1, transport.attachments.Load(), "retired receipt must not attach sources")
		assertSavedKnowledgeLinks(t, f, 0)
		if deletedOriginal != 0 {
			var originalText string
			require.NoError(t, f.db.QueryRow(ctx, `SELECT COALESCE(b.body,e.text) FROM core.conversation_events e
 LEFT JOIN core.conversation_message_bodies b ON b.event_id=e.id WHERE e.id=$1`, deletedOriginal).Scan(&originalText))
			require.Empty(t, originalText, "receipt recovery cannot restore a deleted original request")
		}
	case scenario == "permission_revoked" && name == knowledge.Curate:
		require.Error(t, f.b.Handle(ctx, update), "current mutation permission is checked before finalization")
		require.EqualValues(t, 1, transport.attachments.Load())
		assertSavedKnowledgeLinks(t, f, 0)
		assertSavedKnowledgeReady(t, f, updateID)
	default:
		require.EqualError(t, f.b.Handle(ctx, update), "core API unavailable")
		require.EqualValues(t, 2, transport.attachments.Load())
		assertSavedKnowledgeReady(t, f, updateID)
		assertSavedKnowledgeLinks(t, f, 0)
		require.NoError(t, f.b.Handle(ctx, update))
		assertSavedKnowledgeLinks(t, f, 1)
		var sourceOwner, text string
		require.NoError(t, f.db.QueryRow(ctx,
			`SELECT s.source_owner, COALESCE(b.body,e.text) FROM core.memory_sources s
 JOIN core.conversation_events e ON e.id=s.event_id AND e.owner=s.source_owner
 LEFT JOIN core.conversation_message_bodies b ON b.event_id=e.id
 WHERE s.item_key='recovery'`).Scan(&sourceOwner, &text))
		require.Equal(t, "bob", sourceOwner)
		require.Equal(t, update.Message.Text, text)
		require.NoError(t, f.b.Handle(ctx, update))
		assertSavedKnowledgeLinks(t, f, 1)
	}
	require.EqualValues(t, 1, transport.effects.Load(), "recovery does not execute the domain command")
	require.Empty(t, model.inputs, "recovery does not replan")
	require.Zero(t, model.assessments, "recovery does not assess")
	var operations int
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM core.knowledge_operations WHERE actor='bob'").Scan(&operations),
	)
	require.Equal(t, 1, operations)
}
func assertSavedKnowledgeLinks(t *testing.T, f *fixture, want int) {
	t.Helper()
	var links int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.memory_sources WHERE item_key='recovery'").Scan(&links),
	)
	require.Equal(t, want, links)
}
func assertSavedKnowledgeReady(t *testing.T, f *fixture, updateID int64) {
	t.Helper()
	saved, err := (interaction.Store{DB: f.db}).Load(t.Context(), "bob", updateID)
	require.NoError(t, err)
	require.Equal(t, interaction.Ready, saved.State)
	var replies int
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		"SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='knowledge_reply'",
		updateID,
	).Scan(&replies))
	require.Zero(t, replies, "unfinished attachment must not record a terminal reply")
}
