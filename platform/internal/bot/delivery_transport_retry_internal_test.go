package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
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

func TestBotTransportRetryBudgetAndFollower(t *testing.T) {
	t.Parallel()
	for _, rateLimited := range []bool{false, true} {
		name := "lost_response"
		if rateLimited {
			name = "rate_limit_after_uncertainty"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			ctx := t.Context()
			b := botDeliveryTestBot(db)
			b.Delivery.BotID = 999
			b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
			fake, err := sandbox.New(ctx, db, "synthetic")
			require.NoError(t, err)
			var calls atomic.Int32
			handler := fake.Handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := calls.Add(1)
				if count > 4 {
					handler.ServeHTTP(w, r)
					return
				}
				if rateLimited && count > 1 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					retryAfter := 1
					if count == 2 {
						retryAfter = 3
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": http.StatusTooManyRequests,
						"description": "retry later", "parameters": map[string]int{"retry_after": retryAfter}})
					return
				}
				handler.ServeHTTP(httptest.NewRecorder(), r)
				connection, _, hijackErr := http.NewResponseController(w).Hijack()
				if hijackErr == nil {
					_ = connection.Close()
				}
			}))
			defer server.Close()
			b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
			reference := botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
				Notice: i18n.IdentityUnavailable, Language: "en"}
			first, err := b.enqueueBotIntent(ctx, "", 101, "budget", "notice", reference, "send")
			require.NoError(t, err)
			reference.Update = 2
			follower, err := b.enqueueBotIntent(ctx, "", 101, "follower", "notice", reference, "send")
			require.NoError(t, err)
			for wire := 1; wire <= 4; wire++ {
				started := time.Now()
				require.NoError(t, b.DeliverBotIntent(ctx, first.Reference))
				intent, readErr := botdelivery.Read(ctx, db, 999, first.Reference, false)
				require.NoError(t, readErr)
				var resends int
				require.NoError(t, db.QueryRow(ctx, `SELECT uncertain_resends FROM bot.delivery_intents
 WHERE operation_key='budget'`).Scan(&resends))
				require.Equal(t, wire-1, resends)
				require.EqualValues(t, wire, calls.Load())
				if wire == 4 {
					require.Equal(t, delivery.Rejected, intent.State)
					var reason string
					require.NoError(
						t,
						db.QueryRow(ctx, `SELECT reason FROM bot.delivery_intents WHERE operation_key='budget'`).
							Scan(&reason),
					)
					require.Equal(t, "telegram_uncertain_retry_exhausted", reason)
					time.Sleep(time.Until(intent.NotBefore) + 20*time.Millisecond)
					break
				}
				require.Equal(t, delivery.Deferred, intent.State)
				minimum := time.Second * time.Duration(1<<(wire-1))
				require.GreaterOrEqual(t, intent.NotBefore.Sub(started), minimum)
				if rateLimited && wire == 2 {
					require.GreaterOrEqual(
						t,
						intent.NotBefore.Sub(started),
						3*time.Second,
						"later provider deadline wins",
					)
				}
				require.NoError(t, b.DeliverBotIntent(ctx, first.Reference))
				require.NoError(t, b.DeliverBotIntent(ctx, follower.Reference))
				require.EqualValues(t, wire, calls.Load(), "cooldown and FIFO do not consume wire attempts")
				restarted := botDeliveryTestBot(db)
				restarted.Delivery, restarted.TG = b.Delivery, b.TG
				restarted.Host.LocalBotDelivery.Service.Delivery = b.Delivery
				require.NoError(t, restarted.RecoverBotIntents(ctx))
				b = restarted
				time.Sleep(time.Until(intent.NotBefore) + 20*time.Millisecond)
			}
			require.NoError(t, b.RecoverBotIntents(ctx))
			require.NoError(t, b.DeliverBotIntent(ctx, first.Reference))
			require.EqualValues(t, 4, calls.Load(), "exhausted delivery never revives")
			require.NoError(t, b.DeliverBotIntent(ctx, follower.Reference))
			require.EqualValues(t, 5, calls.Load(), "terminal head releases its follower")
			var lastAttempt int
			require.NoError(t, db.QueryRow(ctx, `SELECT last_uncertain_attempt FROM bot.delivery_intents
 WHERE operation_key='budget'`).Scan(&lastAttempt))
			expectedLast := 4
			if rateLimited {
				expectedLast = 1
			}
			require.Equal(t, expectedLast, lastAttempt, "confirmed rate limits must not overwrite factual uncertainty")
		})
	}
}

func TestBotTransportRetryAdmissionAndRecoveryBudget(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	reference := botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
		Notice: i18n.IdentityUnavailable, Language: "en"}
	queued, err := b.enqueueBotIntent(ctx, "", 101, "recover-budget", "notice", reference, "send")
	require.NoError(t, err)
	current, err := botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	first, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, b.finishBotIntent(ctx, first, delivery.Outcome{Kind: delivery.Deferred,
		Reason: "telegram_rate_limit", RetryAfter: 1}, botdelivery.Continuation{}, false))
	var resends int
	require.NoError(t, db.QueryRow(ctx, `SELECT uncertain_resends FROM bot.delivery_intents
 WHERE operation_key='recover-budget'`).Scan(&resends))
	require.Zero(t, resends, "rate limit before uncertainty retains the original policy")
	current, err = botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	time.Sleep(time.Until(current.NotBefore) + 20*time.Millisecond)
	next, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	// Reproduce a pre-policy unknown head under its existing owner transaction.
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, delivery.Project(ctx, tx, b.Delivery.BotID, queued.Reference, delivery.Uncertain, time.Time{}))
	_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET state='unknown',reason='telegram_outcome_unknown'
 WHERE operation_key='recover-budget'`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, b.RecoverBotIntents(ctx))
	current, err = botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, current.State)
	require.Equal(t, next.Attempt, current.Attempt)
	time.Sleep(time.Until(current.NotBefore) + 20*time.Millisecond)
	_, err = db.Exec(
		ctx,
		`UPDATE core.delivery_pacing SET pause_reason='telegram_bot_unauthorized' WHERE bot_id=$1 AND chat=''`,
		b.Delivery.BotID,
	)
	require.NoError(t, err)
	_, ready, err = b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.False(t, ready)
	require.NoError(t, db.QueryRow(ctx, `SELECT uncertain_resends FROM bot.delivery_intents
 WHERE operation_key='recover-budget'`).Scan(&resends))
	require.Zero(t, resends, "pause before wire admission consumes no resend")
	_, err = db.Exec(
		ctx,
		`UPDATE core.delivery_pacing SET pause_reason='' WHERE bot_id=$1 AND chat=''`,
		b.Delivery.BotID,
	)
	require.NoError(t, err)
	var pausedDeadline time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT not_before FROM core.delivery_queue
 WHERE bot_id=$1 AND owner_key='recover-budget'`, b.Delivery.BotID).Scan(&pausedDeadline))
	time.Sleep(time.Until(pausedDeadline) + 20*time.Millisecond)
	admitted, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, b.RecoverBotIntents(ctx), "crash after Begin keeps its already consumed resend")
	require.NoError(t, b.RecoverBotIntents(ctx))
	var unknownAttempt int64
	require.NoError(t, db.QueryRow(ctx, `SELECT uncertain_resends,last_uncertain_attempt FROM bot.delivery_intents
 WHERE operation_key='recover-budget'`).Scan(&resends, &unknownAttempt))
	require.Equal(t, 1, resends)
	require.Equal(t, admitted.Attempt, unknownAttempt)
}
