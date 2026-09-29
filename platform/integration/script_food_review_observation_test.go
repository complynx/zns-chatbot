package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestFoodReviewRejectsForgedCompletion(t *testing.T) {
	t.Parallel()
	f, s, order := foodSubmittedFixture(t)
	safe := order
	safe.MealPayment.ProofID, safe.ActivityPayment.ProofID = "", ""
	data, err := json.Marshal(safe)
	require.NoError(t, err)
	cursor := core.EncodeReadCursor(
		core.ReadCursor{
			Actor:  "bob",
			Scope:  "food.review.read:food-bot:" + order.ID,
			Offset: len([]rune(string(data))),
		},
	)
	code := fmt.Sprintf(
		`let read=false,accepted=false;try{tools.food.review.read({order_id:%q,cursor:%q});read=true;}catch(_){}try{tools.food.review.decide({kind:"meals",decision:"accept"});accepted=true;}catch(_){}return {read,accepted};`,
		order.ID,
		cursor,
	)
	result := runFoodAdminVM(t, f, 29200, identity.BobTelegramID, code)
	assert.JSONEq(t, `{"read":false,"accepted":false}`, string(result))
	got, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, got.MealPayment.Status)
}

func TestFoodReviewCompleteSequentialChunks(t *testing.T) {
	t.Parallel()
	f, _, order := foodSubmittedFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.food_orders SET meals=$2 WHERE id=$1`,
		order.ID,
		map[string]any{"large": map[string]any{"lunch": map[string]any{"type": strings.Repeat("x", 4200)}}},
	)
	require.NoError(t, err)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			raw := scriptCall(ctx, t, callback, "food.review.read", fmt.Sprintf(`{"order_id":%q}`, order.ID))
			var first core.ReadChunk
			require.NoError(t, json.Unmarshal(raw, &first))
			require.True(t, first.More)
			_, callErr := callback(
				ctx,
				scriptclient.ToolCall{
					Name:      "food.review.decide",
					Arguments: json.RawMessage(`{"kind":"meals","decision":"accept"}`),
				},
			)
			require.Error(t, callErr)
			raw = scriptCall(
				ctx,
				t,
				callback,
				"food.review.read",
				fmt.Sprintf(`{"order_id":%q,"cursor":%q}`, order.ID, first.NextCursor),
			)
			var last core.ReadChunk
			require.NoError(t, json.Unmarshal(raw, &last))
			require.False(t, last.More)
			require.True(t, json.Valid([]byte(first.JSON+last.JSON)))
			scriptCall(ctx, t, callback, "food.review.decide", `{"kind":"meals","decision":"accept"}`)
			return json.RawMessage(`{"accepted":true}`), nil
		},
	)
	runFoodReviewHost(t, f, 29201)
}

func runFoodReviewHost(t *testing.T, f *fixture, update int64) {
	t.Helper()
	f.b.Model = &knowledgeModel{
		plans: []agent.Plan{
			{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
			{View: agent.OrdersView, Text: "Done"},
		},
	}
	handle(t, f.b, message(update, identity.BobTelegramID, "Review the requested meal payment"))
}

type foodObservedProofTransport struct{ before func() }

func (transport foodObservedProofTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/proof") {
		transport.before()
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestFoodReviewProofChecksVersionAtByteRead(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"replacement", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, _, order := foodSubmittedFixture(t)
			f.b.API.HTTP = &http.Client{Transport: foodObservedProofTransport{before: func() {
				if mode == "revoked" {
					_, err := f.db.Exec(t.Context(), `UPDATE core.food_admins SET can_review=false WHERE owner='bob'`)
					require.NoError(t, err)
					return
				}
				_, err := f.db.Exec(
					t.Context(),
					`INSERT INTO core.food_payments(order_id,kind,generation,status,proof_id,received_at) VALUES($1,'meals',2,'proof_submitted','review-proof',now())`,
					order.ID,
				)
				require.NoError(t, err)
				_, err = f.db.Exec(t.Context(), `UPDATE core.food_orders SET version=version+1 WHERE id=$1`, order.ID)
				require.NoError(t, err)
			}}}
			f.b.Host.HTTP = f.b.API.HTTP
			code := fmt.Sprintf(
				`tools.food.review.read({order_id:%q});let displayed=false;try{displayed=tools.food.review.proof({kind:"meals"}).displayed;}catch(_){}return {displayed};`,
				order.ID,
			)
			result := runFoodAdminVM(t, f, 29202, identity.BobTelegramID, code)
			if mode == "revoked" {
				assert.JSONEq(t, `{"omitted":true,"reason":"pass_access_changed"}`, string(result))
				var redacted bool
				require.NoError(t, f.db.QueryRow(
					t.Context(),
					`SELECT COALESCE((content->0->>'pass_redacted')::boolean,false) FROM bot.interactions WHERE owner='bob' AND update_id=29202 AND kind='script_runs'`,
				).Scan(&redacted))
				assert.True(t, redacted, "source revocation must remain terminal in the saved script")
			} else {
				assert.JSONEq(t, `{"displayed":false}`, string(result))
			}
			for _, message := range chatMessages(t, f, 202) {
				assert.Nil(t, message.Document)
			}
		})
	}
}
