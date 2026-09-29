package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestCausalHistoryDeletionInvalidatesArchivedDerivative(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	service := conversation.Service{DB: f.db}
	require.NoError(t, service.AppendOriginal(ctx, "alice", "causal-original", "user", "original private canary"))
	page, err := service.Read(ctx, "alice", conversation.Query{Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	original := page.Events[0].ID
	fullOriginal := strings.Repeat("imported original assistant body ", 220)
	require.NoError(t, service.AppendOriginal(ctx, "alice", "imported-original", "assistant", fullOriginal))
	require.NoError(
		t,
		service.AppendDerived(
			ctx,
			"alice",
			"causal-derived",
			strings.Repeat("derived private canary ", 260),
			0,
			[]readsource.Authority{},
		),
	)
	var derivedID, importedID int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='causal-derived'`).
			Scan(&derivedID),
	)
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='imported-original'`).
			Scan(&importedID),
	)
	require.NoError(t, service.AppendTrustedOutcome(ctx, "alice", "causal-receipt", "committed receipt survives"))
	require.NoError(t, service.DeleteContent(ctx, "alice", original))
	after, err := service.Read(ctx, "alice", conversation.Query{Limit: 10})
	require.NoError(t, err)
	encoded, err := json.Marshal(after)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "committed receipt survives")
	require.NotContains(t, string(encoded), "derived private canary")
	body, err := service.ReadText(ctx, "alice", derivedID, 0, conversation.MaxChunkCharacters, "")
	require.NoError(t, err)
	require.Empty(t, body.Text)
	require.True(t, body.Omitted)
	originalBody, err := service.ReadText(ctx, "alice", importedID, 0, conversation.MaxChunkCharacters, "")
	require.NoError(t, err)
	require.Equal(t, fullOriginal[:conversation.MaxChunkCharacters], originalBody.Text)
	window, err := service.Window(ctx, "alice", 10)
	require.NoError(t, err)
	encoded, err = json.Marshal(window)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "derived private canary")
	batch, err := service.SummaryBatch(ctx, "alice", 1000000)
	require.NoError(t, err)
	encoded, err = json.Marshal(batch)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "derived private canary")
}

type causalReviewVM struct{ afterRead func() error }

func (causalReviewVM) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	panic("unexpected evaluation")
}

func (vm causalReviewVM) Execute(
	ctx context.Context,
	request scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	return scriptworker.Execute(
		ctx,
		scriptprotocol.ExecuteRequest{Code: request.Code, Input: request.Input, Tools: tools},
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			result, err := callback(ctx, call)
			if err == nil && call.Name == "knowledge.review_queue" && vm.afterRead != nil {
				err = vm.afterRead()
				vm.afterRead = nil
			}
			return result, err
		},
	)
}

func TestCausalReviewCannotBeLaunderedIntoMemory(t *testing.T) {
	t.Parallel()
	for _, timing := range []string{"before_write", "after_write"} {
		t.Run(timing, func(t *testing.T) {
			t.Parallel()
			f := knowledgeAuthorizationFixture(t)
			ctx := t.Context()
			revoke := func() error {
				_, err := f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
				return err
			}
			vm := causalReviewVM{}
			if timing == "before_write" {
				vm.afterRead = revoke
			}
			f.b.Scripts = vm
			model := &knowledgeModel{plans: []agent.Plan{
				{
					View: agent.KnowledgeView,
					ScriptAction: &agent.ScriptProposal{
						Code:      `const p=tools.knowledge.review_queue({});return tools.memory.write({name:"document_set",topic:"travel",key:"review-copy",text:p.items[0].text});`,
						InputJSON: "null",
					},
				},
				{View: agent.KnowledgeView, Text: "Done"},
			}}
			f.b.Model = model
			handleErr := f.b.Handle(
				ctx,
				message(7410, identity.BobTelegramID, "Review the queue and save a private summary"),
			)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT count(*) FROM core.memory_documents WHERE owner='bob' AND document_key='review-copy' AND active`).
					Scan(&count),
			)
			if timing == "before_write" {
				require.Zero(t, count, "revoked script source must block new effects")
				return
			}
			if count != 1 {
				var diagnostic json.RawMessage
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=7410 AND kind='script_runs'`).
						Scan(&diagnostic),
				)
				t.Logf("handle=%v script=%s", handleErr, diagnostic)
			}
			require.Equal(t, 1, count, "authorized write must have committed before revocation")
			require.NoError(t, revoke())
			page, err := (knowledge.Service{DB: f.db}).SearchMemory(
				ctx,
				"bob",
				knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "travel"},
			)
			require.NoError(t, err)
			encoded, err := json.Marshal(page)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "Private pending proposal")
		})
	}
}

func TestCausalSummaryRechecksSourceBeforeExposure(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	ctx := t.Context()
	service := conversation.Service{DB: f.db}
	refs := []readsource.Authority{{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}}}
	require.NoError(t, service.AppendDerived(ctx, "bob", "summary-private", "private review summary canary", 0, refs))
	require.NoError(t, service.AppendOriginal(ctx, "bob", "summary-newer", "user", "unrelated recent message"))
	f.b.HistoryLimit = 1
	model := &historyModel{plans: []agent.Plan{{View: "workflow", Text: "Done"}}}
	f.b.Model = model
	revoked := false
	f.b.API.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(r)
		if err == nil && r.URL.Path == "/internal/history/summary-batch" && !revoked {
			revoked = true
			_, err = f.db.Exec(r.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
		}
		return response, err
	})}
	f.b.Host.HTTP = f.b.API.HTTP
	_ = f.b.Handle(ctx, message(7411, identity.BobTelegramID, "Recall our earlier conversation"))
	require.True(t, revoked, "revoke after authoritative batch read must execute")
	encoded, err := json.Marshal(model.summaries)
	require.NoError(t, err)
	require.NotContains(
		t,
		string(encoded),
		"private review summary canary",
		"rejecting summary commit is too late to prevent exposure",
	)
}
