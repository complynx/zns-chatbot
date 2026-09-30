package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
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
	transport := &foodExportRetry{}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	f.b.Delivery.Fallback = 10 * time.Millisecond
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
	pumpBotDeliveries(t, f.b)
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
	code := fmt.Sprintf(
		`try { tools.food.export({continuation:%q}); return {denied:false}; } catch (_) { return {denied:true}; }`,
		result.Continuation,
	)
	raw = runFoodAdminVM(t, f, 48102, identity.BobTelegramID, code)
	require.JSONEq(t, `{"denied":true}`, string(raw))
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
	raw = runFoodAdminVM(t, f, 48103, identity.BobTelegramID, code)
	require.JSONEq(
		t,
		`{"denied":true}`,
		string(raw),
		"restoring a grant does not resurrect the deleted source generation",
	)
	waitFoodExportRetry(t, f, 48101)
	pumpBotDeliveries(t, f.b)
	require.EqualValues(t, 2, transport.documents.Load())
	raw = runFoodAdminVM(t, f, 48104, identity.BobTelegramID, `return tools.food.export({});`)
	require.NoError(t, json.Unmarshal(raw, &result))
	require.False(t, result.Complete, "enqueue alone is not a transport receipt")
	pumpBotDeliveries(t, f.b)
	raw = runFoodAdminVM(
		t,
		f,
		48105,
		identity.BobTelegramID,
		fmt.Sprintf(`return tools.food.export({continuation:%q});`, result.Continuation),
	)
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
	handleVisible(t, f.b, message(48110, identity.BobTelegramID, "Export requested food"))
	require.Equal(t, 1, transport.sent)
	assertFoodExportStopped(t, f, 48110)
	require.Equal(t, 1, transport.sent, "cancelled work must not resend")
}

func assertFoodExportStopped(t *testing.T, f *fixture, update int64) {
	t.Helper()
	var sent, cancelled, projections int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
count(*) FILTER (WHERE state='sent' AND message_id>0 AND continuation_done
AND receipt->'document'->>'sha256' ~ '^[a-f0-9]{64}$' AND (receipt->'document'->>'bytes')::bigint>0),
count(*) FILTER (WHERE state='cancelled') FROM bot.delivery_intents
WHERE owner='bob' AND reference->>'update'=$1 AND reference->>'kind'='document'`, strconv.FormatInt(update, 10)).Scan(&sent, &cancelled))
	require.Equal(t, 1, sent, "the completed wire effect retains its exact durable receipt")
	require.Equal(t, 1, cancelled, "the next file must be cancelled after source revocation")
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions
WHERE owner='bob' AND update_id=$1 AND kind IN ('food_orders_export','food_summary_export')`, update).Scan(&projections))
	require.Zero(t, projections, "receipt projection must not re-expose a revoked private source")
	pumpBotDeliveries(t, f.b)
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
	handleVisible(t, f.b, message(48120, identity.BobTelegramID, "Display requested food proof"))
	var state string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state FROM bot.delivery_intents
WHERE owner='bob' AND reference->>'family'='food_review_meals' AND reference->>'update'='48120'`).Scan(&state))
	require.Equal(t, string(delivery.Cancelled), state)
	terminal, err := (interaction.Store{DB: f.db}).Load(t.Context(), "bob", 48120)
	require.NoError(t, err)
	require.Equal(t, interaction.HistoryDeleted, terminal.TerminalReason)
	require.Equal(t, interaction.PrivacyTerminal, terminal.State)
	require.True(t, transport.called)
	for _, sent := range chatMessages(t, f, identity.BobTelegramID) {
		require.Nil(t, sent.Document)
	}
}

func TestFoodExportAdmissionKeepsImmutableIdentity(t *testing.T) {
	t.Parallel()
	f, _, _ := foodSubmittedFixture(t)
	raw := runFoodAdminVM(t, f, 48130, identity.BobTelegramID, `return tools.food.export({});`)
	var result struct {
		Continuation string `json:"continuation"`
		Complete     bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.NotEmpty(t, result.Continuation)
	require.False(t, result.Complete)
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
WHERE owner='bob' AND update_id=48130 AND kind='script_runs'`).Scan(&records))
	require.Len(t, records, 1)
	require.Empty(t, records[0].Run.Error)
	require.Len(t, records[0].Calls, 1)
	require.Empty(t, records[0].Calls[0].Outcome.Error)
	require.NotNil(t, records[0].Calls[0].FoodExport)
	require.Nil(t, records[0].Calls[0].FoodExport.Source, "completion must not rewrite the admitted request identity")
	var saved agenthost.FoodExportRequest
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
WHERE owner='bob' AND update_id=48130 AND kind='food_export_request'`).Scan(&saved))
	require.NotNil(t, saved.Source)
	require.True(t, saved.Source.Valid(), "the separate continuation must retain its admitted source")
	var queued int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
WHERE owner='bob' AND reference->>'update'='48130' AND reference->>'kind'='document'
AND reference->'source'=$1::jsonb`, saved.Source).Scan(&queued))
	require.Equal(t, 2, queued)
}
