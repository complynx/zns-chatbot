package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestCausalKnowledgeLostResponseResumesFiltering(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"authorized", "saved_verdict", "source_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := knowledgeAuthorizationFixture(t)
			ctx := t.Context()
			const updateID int64 = 88601
			const secret = "KNOWLEDGE-RECOVERY-PRIVATE-SOURCE"
			plan := interaction.SavedPlan{
				Plan: agent.Plan{View: agent.KnowledgeView},
				KnowledgeCommand: &knowledge.Command{
					Name:    knowledge.Suggest,
					Topic:   "travel",
					FactKey: "recovery",
					Text:    secret,
				},
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
			transport := &savedMutationResponseLoss{path: "/internal/knowledge/derived"}
			f.b.Host.HTTP = &http.Client{Transport: transport}
			model := &knowledgeModel{}
			f.b.Model = model
			update := message(updateID, identity.BobTelegramID, "Save the selected suggestion")
			require.Error(t, f.b.Handle(ctx, update))
			require.EqualValues(t, 1, transport.calls.Load())
			require.Zero(t, model.assessments, "lost response must occur before filtering")
			require.Empty(t, model.inputs, "the saved plan needs no new planning")
			assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
			if scenario == "source_revoked" {
				_, err = f.db.Exec(
					ctx,
					`DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review'`,
				)
				require.NoError(t, err)
			}
			if scenario == "saved_verdict" {
				_, err = f.db.Exec(
					ctx,
					`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('bob',$1,'knowledge_assessment',$2)`,
					updateID,
					agent.KnowledgeAssessment{Worthwhile: true, Reason: "Already classified"},
				)
				require.NoError(t, err)
			}
			restarted := &knowledgeModel{}
			f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Model: restarted}
			require.NoError(t, f.b.Handle(ctx, update))
			wantCalls := 0
			if scenario == "authorized" {
				wantCalls = 1
			}
			require.Equal(t, wantCalls, restarted.assessments)
			require.Empty(t, restarted.inputs)
			if scenario == "source_revoked" {
				assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
				_, err = f.db.Exec(
					ctx,
					`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
				)
				require.NoError(t, err)
			} else {
				assertKnowledgeRecoveryState(t, f, knowledge.AwaitingSubmission, 2)
			}
			require.NoError(t, f.b.Handle(ctx, update))
			require.Equal(t, wantCalls, restarted.assessments, "replay must reuse the verdict")
			cards, err := json.Marshal(chatMessages(t, f, identity.BobTelegramID))
			require.NoError(t, err)
			if scenario == "source_revoked" {
				require.NotContains(t, string(cards), secret)
				var archived int
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.conversation_events e LEFT JOIN core.conversation_message_bodies b ON b.event_id=e.id WHERE e.owner='bob' AND e.origin='derived' AND COALESCE(b.body,e.text) LIKE '%KNOWLEDGE-RECOVERY-PRIVATE-SOURCE%'`).
						Scan(&archived),
				)
				require.Zero(t, archived)
				assertKnowledgeRecoveryState(t, f, "pending_filter", 1)
			} else {
				require.Contains(t, string(cards), secret, "authorized proposal remains visible")
			}
		})
	}
}

func assertKnowledgeRecoveryState(t *testing.T, f *fixture, want string, operations int) {
	t.Helper()
	var state string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT state FROM core.knowledge_proposals WHERE owner='bob' AND fact_key='recovery'`).
			Scan(&state),
	)
	require.Equal(t, want, state)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='bob'`).Scan(&count),
	)
	require.Equal(t, operations, count)
}
