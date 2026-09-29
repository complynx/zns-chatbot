package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestFoodDerivedExportContinuationRetainsSource(t *testing.T) {
	t.Parallel()
	f, _, _ := foodSubmittedFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	transport := &foodExportSecondFailure{}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	raw := runFoodAdminVM(
		t,
		f,
		48101,
		identity.BobTelegramID,
		`tools.knowledge.review_queue({});return tools.food.export({});`,
	)
	var result struct {
		Continuation string `json:"continuation"`
		Complete     bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.NotEmpty(t, result.Continuation)
	require.False(t, result.Complete)
	require.EqualValues(t, 2, transport.documents.Load())
	var saved struct {
		Source *readsource.Derivation `json:"source"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=48101 AND kind='food_export_request'`).
			Scan(&saved),
	)
	require.NotNil(t, saved.Source)
	require.True(t, saved.Source.Valid())
	require.NotEmpty(t, saved.Source.Authorities)
	original, err := json.Marshal(saved.Source)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	restarted := *f.b
	f.b = &restarted
	code := fmt.Sprintf(`return tools.food.export({continuation:%q});`, result.Continuation)
	raw = runFoodAdminVM(t, f, 48102, identity.BobTelegramID, code)
	require.NoError(t, json.Unmarshal(raw, &result))
	require.False(t, result.Complete)
	require.EqualValues(t, 2, transport.documents.Load())
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=48101 AND kind='food_export_request'`).
			Scan(&saved),
	)
	after, err := json.Marshal(saved.Source)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(after))
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	currentGeneration, generationErr := f.b.API.HistoryGeneration(t.Context(), "bob")
	require.NoError(t, generationErr)
	t.Logf(
		"saved export generation=%d; current generation after source loss=%d",
		*saved.Source.Generation,
		currentGeneration,
	)
	raw = runFoodAdminVM(t, f, 48103, identity.BobTelegramID, code)
	require.NoError(t, json.Unmarshal(raw, &result))
	require.False(t, result.Complete, "restoring a grant does not resurrect the deleted source generation")
	require.EqualValues(t, 2, transport.documents.Load())
	raw = runFoodAdminVM(t, f, 48104, identity.BobTelegramID, `return tools.food.export({});`)
	require.NoError(t, json.Unmarshal(raw, &result))
	require.True(t, result.Complete, "a fresh authorized request uses the current generation")
	require.EqualValues(t, 4, transport.documents.Load())
}

func TestFoodDerivedExportChecksSourceBetweenFiles(t *testing.T) {
	t.Parallel()
	f, _, _ := foodSubmittedFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	transport := &foodRevokeAfterDocument{after: func() {
		_, revokeErr := f.db.Exec(context.Background(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
		require.NoError(t, revokeErr)
	}}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	f.b.Scripts = scopeVM{}
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{
			View: agent.OrdersView,
			ScriptAction: &agent.ScriptProposal{
				Code:      `tools.knowledge.review_queue({});return tools.food.export({});`,
				InputJSON: "null",
			},
		},
		{View: agent.OrdersView, Text: "Completed"},
	}}
	handle(t, f.b, message(48110, identity.BobTelegramID, "Export requested food"))
	require.Equal(t, 1, transport.sent)
	var receipts int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=48110 AND kind IN ('food_orders_export','food_summary_export')`).
			Scan(&receipts),
	)
	require.Equal(t, 1, receipts)
}

type foodProofAfterRead struct {
	after  func()
	called bool
}

func (r *foodProofAfterRead) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil && strings.HasSuffix(request.URL.Path, "/proof") && !r.called {
		r.called = true
		r.after()
	}
	return response, err
}

func TestFoodDerivedProofChecksSourceAfterDownload(t *testing.T) {
	t.Parallel()
	f, _, order := foodSubmittedFixture(t)
	history := conversation.Service{DB: f.db}
	require.NoError(t, history.AppendOriginal(t.Context(), "bob", "food-proof-source", "user", "original"))
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='food-proof-source'`).
			Scan(&id),
	)
	transport := &foodProofAfterRead{
		after: func() { require.NoError(t, history.DeleteContent(t.Context(), "bob", id)) },
	}
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	f.b.Scripts = scopeVM{}
	code := fmt.Sprintf(
		`tools.food.review.read({order_id:%q});return tools.food.review.proof({kind:"meals"});`,
		order.ID,
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Completed"},
	}}
	err := f.b.Handle(t.Context(), message(48120, identity.BobTelegramID, "Display requested food proof"))
	require.ErrorIs(t, err, appclient.ErrReadStale)
	terminal, err := (interaction.Store{DB: f.db}).Load(t.Context(), "bob", 48120)
	require.NoError(t, err)
	require.Equal(t, interaction.HistoryDeleted, terminal.TerminalReason)
	require.Equal(t, interaction.PrivacyTerminal, terminal.State)
	require.True(t, transport.called)
	for _, sent := range chatMessages(t, f, identity.BobTelegramID) {
		require.Nil(t, sent.Document)
	}
}
