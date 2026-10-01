package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
	"github.com/stretchr/testify/require"
)

func TestBotTransportRetryLostResponse(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.Fallback = time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	var calls atomic.Int32
	handler := fake.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) != 1 {
			handler.ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		// Persist the real provider message, then lose its acknowledgement.
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	reference := botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
		Notice: i18n.IdentityUnavailable, Language: "en"}
	_, err = b.enqueueBotIntent(ctx, "", 101, "lost", "notice", reference, "send")
	require.NoError(t, err)
	key := delivery.Reference{Owner: delivery.Bot, Key: "lost", Effect: "notice"}
	require.NoError(t, b.DeliverBotIntent(ctx, key))
	// UI proves actual delivery despite the missing HTTP response.
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	var visible struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &visible))
	require.Len(t, visible.Messages, 1)
	intent, err := botdelivery.Read(ctx, db, 999, key, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, intent.State, "uncertain transport schedules a resend")
	require.GreaterOrEqual(t, time.Until(intent.NotBefore), 500*time.Millisecond)
	require.NoError(t, b.DeliverBotIntent(ctx, key))
	require.EqualValues(t, 1, calls.Load(), "backoff prevents an immediate resend")
	restarted := botDeliveryTestBot(db)
	restarted.Delivery = b.Delivery
	restarted.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	restarted.TG = b.TG
	require.NoError(t, restarted.RecoverBotIntents(ctx))
	time.Sleep(time.Until(intent.NotBefore) + 20*time.Millisecond)
	require.NoError(t, restarted.DeliverBotIntent(ctx, key))
	intent, err = botdelivery.Read(ctx, db, 999, key, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, intent.State)
	require.True(t, intent.ContinuationDone)
	require.EqualValues(t, 2, calls.Load())
	var attempt, resends int
	var reason string
	require.NoError(t, db.QueryRow(ctx, `SELECT last_uncertain_attempt,last_uncertain_reason,uncertain_resends
 FROM bot.delivery_intents WHERE operation_key='lost'`).Scan(&attempt, &reason, &resends))
	require.Equal(t, 1, attempt)
	require.Equal(t, "telegram_outcome_unknown", reason)
	require.Equal(t, 1, resends)
	state = httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &visible))
	require.Len(t, visible.Messages, 2, "at-least-once delivery allows a duplicate")
}
