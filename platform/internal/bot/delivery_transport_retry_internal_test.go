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
			testBotTransportRetryBudgetAndFollower(t, rateLimited)
		})
	}
}

func testBotTransportRetryBudgetAndFollower(t *testing.T, rateLimited bool) {
	t.Helper()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	var calls atomic.Int32
	handler := fake.Handler()
	server := httptest.NewServer(botTransportRetryBudgetHandler(handler, rateLimited, &calls))
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
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	var visible struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &visible))
	expectedVisible := 5
	if rateLimited {
		expectedVisible = 2
	}
	require.Len(
		t,
		visible.Messages,
		expectedVisible,
		"the UI retains real deliveries and the released follower",
	)
	assertBotTransportRetryTerminalReceipt(t, b, first.Reference, rateLimited, visible.Messages, &calls)
}

func assertBotTransportRetryTerminalReceipt(
	t *testing.T,
	b *Bot,
	reference delivery.Reference,
	rateLimited bool,
	messages []telegram.Message,
	calls *atomic.Int32,
) {
	t.Helper()
	ctx, db := t.Context(), b.DB
	terminal, readErr := botdelivery.Read(ctx, db, 999, reference, false)
	require.NoError(t, readErr)
	late := delivery.Outcome{Kind: delivery.Succeeded, MessageID: messages[0].ID}
	if rateLimited {
		require.ErrorIs(t, b.finishBotIntent(ctx, terminal, late, botdelivery.Continuation{}, false),
			botdelivery.ErrBinding, "older uncertain attempt cannot resolve the newer exhausted generation")
	} else {
		late.MessageID = messages[3].ID
		var before, after string
		require.NoError(t, db.QueryRow(ctx, `SELECT (to_jsonb(i)-'message_id')::text
 FROM bot.delivery_intents i WHERE operation_key='budget'`).Scan(&before))
		require.NoError(t, b.finishBotIntent(ctx, terminal, late, botdelivery.Continuation{}, false))
		require.NoError(t, db.QueryRow(ctx, `SELECT (to_jsonb(i)-'message_id')::text
 FROM bot.delivery_intents i WHERE operation_key='budget'`).Scan(&after))
		require.JSONEq(t, before, after, "terminal receipt changes only the message ID")
		require.NoError(t, b.finishBotIntent(ctx, terminal, late, botdelivery.Continuation{}, false))
	}
	require.NoError(t, b.RecoverBotIntents(ctx))
	require.NoError(t, b.DeliverBotIntent(ctx, reference))
	require.EqualValues(t, 5, calls.Load(), "late receipt cannot revive transport")
}

func botTransportRetryBudgetHandler(handler http.Handler, rateLimited bool, calls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
}

func TestBotTransportRetryCancelledLateReceipt(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	ref := botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
		Notice: i18n.IdentityUnavailable, Language: "en"}
	queued, err := b.enqueueBotIntent(ctx, "", 101, "cancelled-late", "notice", ref, "send")
	require.NoError(t, err)
	current, err := botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	attempt, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, b.RecoverBotIntents(ctx))
	current, err = botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	require.NoError(t, b.postponeBotIntent(ctx, current, true))
	snapshot := func() string {
		var saved string
		require.NoError(t, db.QueryRow(ctx, `SELECT jsonb_build_object(
 'intent',(SELECT to_jsonb(i)-'message_id' FROM bot.delivery_intents i WHERE operation_key='cancelled-late'),
 'queue',(SELECT jsonb_agg(to_jsonb(q)) FROM core.delivery_queue q),
 'pacing',(SELECT jsonb_agg(to_jsonb(p)) FROM core.delivery_pacing p))::text`).Scan(&saved))
		return saved
	}
	before := snapshot()
	late := delivery.Outcome{Kind: delivery.Succeeded, MessageID: 900}
	require.NoError(t, b.finishBotIntent(ctx, attempt, late, botdelivery.Continuation{Kind: "ignored"}, false))
	require.JSONEq(t, before, snapshot())
	require.NoError(t, b.finishBotIntent(ctx, attempt, late, botdelivery.Continuation{}, false))
	conflicting := late
	conflicting.MessageID++
	require.ErrorIs(
		t,
		b.finishBotIntent(ctx, attempt, conflicting, botdelivery.Continuation{}, false),
		botdelivery.ErrBinding,
	)
	wrong := attempt
	wrong.BotID++
	require.ErrorIs(t, b.finishBotIntent(ctx, wrong, late, botdelivery.Continuation{}, false), botdelivery.ErrBinding)
	wrong = attempt
	wrong.Attempt++
	require.ErrorIs(t, b.finishBotIntent(ctx, wrong, late, botdelivery.Continuation{}, false), botdelivery.ErrBinding)
	require.ErrorIs(t, b.finishBotIntent(ctx, attempt, late, botdelivery.Continuation{}, true), botdelivery.ErrBinding)
	require.NoError(t, b.RecoverBotIntents(ctx))
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
	require.NoError(t, b.ContinueBotIntentReceipts(ctx))
	require.JSONEq(t, before, snapshot(), "terminal receipt never resumes continuation or changes queue/pacing")
	current, err = botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Cancelled, current.State)
	require.EqualValues(t, 900, current.MessageID)
	require.False(t, current.ContinuationDone)
}

func TestBotTransportRetryPendingLateReceipt(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	ref := botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
		Notice: i18n.IdentityUnavailable, Language: "en"}
	queued, err := b.enqueueBotIntent(ctx, "", 101, "pending-late", "notice", ref, "send")
	require.NoError(t, err)
	ref.Update++
	follower, err := b.enqueueBotIntent(ctx, "", 101, "pending-follower", "notice", ref, "send")
	require.NoError(t, err)
	current, err := botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	known := delivery.Outcome{Kind: delivery.Succeeded, MessageID: 900}
	require.ErrorIs(
		t,
		b.finishBotIntent(ctx, current, known, botdelivery.Continuation{}, false),
		botdelivery.ErrBinding,
	)
	attempt, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, b.RecoverBotIntents(ctx))
	require.NoError(t, b.finishBotIntent(ctx, attempt, known, botdelivery.Continuation{}, false))
	require.NoError(t, b.ContinueBotIntentReceipts(ctx))
	require.NoError(t, b.ContinueBotIntentReceipts(ctx))
	current, err = botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, current.State)
	require.True(t, current.ContinuationDone)
	require.Equal(t, attempt.Attempt, current.Attempt, "late receipt needs no new wire admission")
	var resends int
	require.NoError(t, db.QueryRow(ctx, `SELECT uncertain_resends FROM bot.delivery_intents
 WHERE operation_key='pending-late'`).Scan(&resends))
	require.Zero(t, resends)
	next, err := botdelivery.Read(ctx, db, b.Delivery.BotID, follower.Reference, false)
	require.NoError(t, err)
	_, ready, err = b.beginBotIntent(ctx, next, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready, "late success releases the follower")
}

func TestBotTransportRetryTerminalResponseWorker(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	ref := botdelivery.Reference{Kind: botdelivery.IdentityIntent, Update: 1,
		Notice: i18n.IdentityUnavailable, Language: "en"}
	queued, err := b.enqueueBotIntent(ctx, "", 101, "worker-terminal", "notice", ref, "send")
	require.NoError(t, err)
	completion := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		fake.Handler().ServeHTTP(recorder, r)
		// Hold the actual provider receipt while the owner records interruption
		// and cancellation. The response then reaches the original worker.
		admitted, readErr := botdelivery.Read(ctx, db, 999, queued.Reference, false)
		if readErr == nil {
			readErr = b.finishBotIntent(ctx, admitted,
				delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
				botdelivery.Continuation{}, false)
		}
		if readErr == nil {
			admitted, readErr = botdelivery.Read(ctx, db, 999, queued.Reference, false)
		}
		if readErr == nil {
			readErr = b.postponeBotIntent(ctx, admitted, true)
		}
		completion <- readErr
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference), "terminal receipt must not invoke sent continuation")
	require.NoError(t, <-completion)
	current, err := botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Cancelled, current.State)
	require.Positive(t, current.MessageID)
	require.False(t, current.ContinuationDone)
	require.NoError(t, b.RecoverBotIntents(ctx))
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
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
