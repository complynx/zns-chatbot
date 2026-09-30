package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestFoodReviewQueuePaginationAndScope(t *testing.T) {
	t.Parallel()
	f, s, original := foodSubmittedFixture(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
 SELECT 'food-review-'||i,50000+i,'Review User',true FROM generate_series(1,25) i;
 INSERT INTO core.food_orders(id,event_id,owner,version,created_at)
 SELECT 'review-'||i,'food-bot','food-review-'||i,1,now() FROM generate_series(1,25) i;
 INSERT INTO core.order_proofs(id,owner,filename,body)
 SELECT 'proof-'||i,'food-review-'||i,'receipt.pdf','x'::bytea FROM generate_series(1,25) i;
 INSERT INTO core.food_payments(order_id,kind,generation,status,proof_id,received_at)
 SELECT 'review-'||i,'meals',1,'proof_submitted','proof-'||i,now() FROM generate_series(1,25) i;
 INSERT INTO core.food_payments(order_id,kind,generation,status) VALUES('review-1','meals',2,'pending')`)
	require.NoError(t, err)
	first, err := s.ReviewQueue(t.Context(), "bob", "food-bot", "")
	require.NoError(t, err)
	require.Len(t, first.Items, core.ReadPageItems)
	require.True(t, first.More)
	second, err := s.ReviewQueue(t.Context(), "bob", "food-bot", first.NextCursor)
	require.NoError(t, err)
	require.Len(t, second.Items, 5)
	require.False(t, second.More)
	ids := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		require.False(t, ids[item.OrderID])
		ids[item.OrderID] = true
	}
	assert.True(t, ids[original.ID])
	assert.False(t, ids["review-1"], "an older submitted generation is not a pending review")
	_, err = s.ReviewQueue(t.Context(), "alice", "food-bot", first.NextCursor)
	requireCode(t, err, "forbidden")
	_, err = f.db.Exec(t.Context(), `UPDATE core.food_events SET bot_id=88 WHERE event_id='food-bot'`)
	require.NoError(t, err)
	_, err = s.ReviewQueue(t.Context(), "bob", "food-bot", first.NextCursor)
	requireCode(t, err, "food_event_unavailable")
}

type foodRevokeAfterDocument struct {
	after func()
	sent  int
}

func (r *foodRevokeAfterDocument) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if strings.HasSuffix(request.URL.Path, "/sendDocument") && err == nil {
		r.sent++
		if r.sent == 1 {
			r.after()
		}
	}
	return response, err
}

func TestScriptFoodExportRechecksBeforeSecondFile(t *testing.T) {
	t.Parallel()
	f, _, _ := foodSubmittedFixture(t)
	transport := &foodRevokeAfterDocument{after: func() {
		_, err := f.db.Exec(context.Background(), `UPDATE core.food_admins SET can_export=false WHERE owner='bob'`)
		require.NoError(t, err)
	}}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	raw := runFoodAdminVM(t, f, 28020, identity.BobTelegramID, `return tools.food.export({});`)
	var result struct {
		Complete     bool   `json:"complete"`
		Continuation string `json:"continuation"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	assert.False(t, result.Complete)
	assert.NotEmpty(t, result.Continuation)
	pumpBotDeliveries(t, f.b)
	assert.Equal(t, 1, transport.sent)
	assertFoodExportStopped(t, f, 28020)
	assert.Equal(t, 1, transport.sent, "terminal delivery replay must not resend")
}
