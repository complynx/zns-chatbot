package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPrivateDeletionReceiptRejectsExternalInvalidation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"none", "permission", "history", "recreate", "other_deletion"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := knowledgeAuthorizationFixture(t)
			service := knowledge.Service{DB: f.db}
			_, err := service.Execute(t.Context(), "bob", knowledge.Command{Name: knowledge.MemoSet,
				Key: "seed", FactKey: "diet", Text: "PRIVATE-MEMO-WITNESS-CANARY"})
			require.NoError(t, err)
			history, err := (conversation.Service{DB: f.db}).Window(t.Context(), "bob", 1)
			require.NoError(t, err)
			source := readsource.Derivation{Generation: &history.Generation, Authorities: []readsource.Authority{
				{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
			}}
			command := knowledge.Command{Name: knowledge.MemoDelete, Key: "delete", FactKey: "diet", Version: 1}
			result, err := service.ExecuteDerived(t.Context(), "bob", command, source)
			require.NoError(t, err)
			require.NotNil(t, result.PrivateDeletion)
			require.False(t, result.PrivateDeletion.Current)
			switch change {
			case "permission":
				_, err = f.db.Exec(
					t.Context(),
					"DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review'",
				)
			case "history":
				_, err = f.db.Exec(
					t.Context(),
					"INSERT INTO core.conversation_history_generations(owner,generation) VALUES('bob',1) ON CONFLICT(owner) DO UPDATE SET generation=core.conversation_history_generations.generation+1",
				)
			case "recreate":
				_, err = service.Execute(
					t.Context(),
					"bob",
					knowledge.Command{
						Name:    knowledge.MemoSet,
						Key:     "recreate",
						FactKey: "diet",
						Text:    "replacement",
						Version: 2,
					},
				)
			case "other_deletion":
				_, err = service.Execute(
					t.Context(),
					"bob",
					knowledge.Command{Name: knowledge.MemoSet, Key: "other", FactKey: "other", Text: "other"},
				)
				require.NoError(t, err)
				_, err = service.Execute(
					t.Context(),
					"bob",
					knowledge.Command{Name: knowledge.MemoDelete, Key: "delete-other", FactKey: "other", Version: 1},
				)
			}
			require.NoError(t, err)
			receipt, found, err := service.CommandReceipt(t.Context(), "bob", command, source)
			require.NoError(t, err)
			require.True(t, found)
			require.NotNil(t, receipt.PrivateDeletion)
			require.Equal(t, change == "none", receipt.PrivateDeletion.Current)
			model, err := json.Marshal(agenthost.ModelKnowledgeResult(receipt))
			require.NoError(t, err)
			require.NotContains(t, string(model), "private_deletion")
			require.NotContains(t, string(model), "PRIVATE-MEMO-WITNESS-CANARY")
			var operations int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), "SELECT count(*) FROM core.knowledge_operations WHERE actor='bob' AND result->'memo'->>'key'='diet' AND result->'memo'->>'version'='2'").
					Scan(&operations),
			)
			require.Equal(t, 1, operations)
		})
	}
}

func TestScriptGroundedDocumentDeletionCompletesWithTombstone(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = scopeVM{}
	_, err := f.b.API.ExecuteKnowledge(t.Context(), "alice", knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "seed-document-delete",
		Topic:   "preferences",
		FactKey: "tea",
		Text:    "DOCUMENT-DELETION-CANARY",
	})
	require.NoError(t, err)
	code := `const page=await tools.memory.search({namespace:"private",topic:"preferences",text:"DOCUMENT-DELETION-CANARY",mode:"literal"});const ref=page.entries[0].ref;await tools.memory.read({ref});await tools.memory.write({name:"document_delete",topic:"preferences",key:"tea",ref});return {done:true};`
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	f.b.Model = model
	update := message(19762, identity.AliceTelegramID, "Delete my saved tea document")
	handle(t, f.b, update)
	state, err := (knowledge.Service{DB: f.db}).DocumentState(t.Context(), "alice", "preferences", "tea")
	require.NoError(t, err)
	require.False(t, state.Active)
	require.Equal(t, int64(2), state.Version)
	require.Len(t, model.inputs, 2)
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "DOCUMENT-DELETION-CANARY")
	require.Len(t, model.inputs[1].Script.Runs, 1)
	require.Empty(t, model.inputs[1].Script.Runs[0].Code)
	require.JSONEq(t, `{"omitted":true,"reason":"memory_deleted"}`, string(model.inputs[1].Script.Runs[0].Result))
	handle(t, f.b, update)
	require.Len(t, model.inputs, 2)
	var deletes int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='alice' AND result->'document'->>'key'='tea' AND result->'document'->>'version'='2'`).
			Scan(&deletes),
	)
	require.Equal(t, 1, deletes)
}
