package bot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestBotTransportRetryCapturedLanguageCard(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.UncertaintyRetryBase = time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	_, err := db.Exec(ctx, `UPDATE core.users SET telegram_id=101,language='en' WHERE id='alice'`)
	require.NoError(t, err)
	preferences := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var language string
		if readErr := db.QueryRow(r.Context(), `SELECT language FROM core.users WHERE id='alice'`).
			Scan(&language); readErr != nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"language": language})
	}))
	defer preferences.Close()
	b.API.Base, b.API.HTTP = preferences.URL, preferences.Client()
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	requests := make(chan []byte, 3)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, botdelivery.MaxRequestBytes+1))
		if readErr != nil {
			http.Error(w, "unavailable", http.StatusBadRequest)
			return
		}
		requests <- body
		r.Body = io.NopCloser(bytes.NewReader(body))
		if calls.Add(1) == 1 {
			fake.Handler().ServeHTTP(httptest.NewRecorder(), r)
			connection, _, hijackErr := http.NewResponseController(w).Hijack()
			if hijackErr == nil {
				_ = connection.Close()
			}
			return
		}
		fake.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	ref := botdelivery.Reference{Kind: botdelivery.CardIntent, Family: languageKey, CardKey: languageKey}
	require.NoError(t, b.bindBotCardSource(ctx, "alice", &ref))
	queued, err := b.enqueueBotIntent(ctx, "alice", 101, "captured-language", "view", ref, "send")
	require.NoError(t, err)
	testBotCaptureDeliverReady(t, &b, queued.Reference)
	original := testBotCaptureRequest(t, requests)
	_, err = db.Exec(ctx, `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	current, err := botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	restarted := botDeliveryTestBot(db)
	restarted.Delivery, restarted.API, restarted.TG = b.Delivery, b.API, b.TG
	restarted.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	require.NoError(t, restarted.RecoverBotIntents(ctx))
	require.Equal(t, delivery.Deferred, current.State)
	testBotCaptureDeliverReady(t, &restarted, queued.Reference)
	require.JSONEq(
		t,
		string(original),
		string(testBotCaptureRequest(t, requests)),
		"resend preserves the original normalized text and keyboard",
	)
	require.NoError(t, restarted.bindBotCardSource(ctx, "alice", &ref))
	_, err = restarted.enqueueBotIntent(ctx, "alice", 101, "fresh-language", "view", ref, "send")
	require.NoError(t, err)
	testBotCaptureDeliverReady(t, &restarted,
		delivery.Reference{Owner: delivery.Bot, Key: "fresh-language", Effect: "view"})
	fresh := testBotCaptureRequest(t, requests)
	require.NotEqual(t, string(original), string(fresh), "new intents render current Russian preferences")
	var originalPayload, freshPayload telegram.Send
	require.NoError(t, json.Unmarshal(original, &originalPayload))
	require.NoError(t, json.Unmarshal(fresh, &freshPayload))
	require.NotEqual(t, originalPayload.Text, freshPayload.Text)
	require.EqualValues(t, 3, calls.Load())
}

func testBotCaptureDeliverReady(t *testing.T, b *Bot, key delivery.Reference) {
	t.Helper()
	var notBefore time.Time
	require.NoError(t, b.DB.QueryRow(t.Context(), `SELECT GREATEST(i.not_before,
 (SELECT not_before FROM core.delivery_pacing WHERE bot_id=i.bot_id AND chat=''),
 (SELECT not_before FROM core.delivery_pacing WHERE bot_id=i.bot_id AND chat=i.chat_id::text))
 FROM bot.delivery_intents i WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		b.Delivery.BotID, key.Key, key.Effect).Scan(&notBefore))
	wait := time.NewTimer(max(time.Until(notBefore), 0) + 20*time.Millisecond)
	defer wait.Stop()
	select {
	case <-wait.C:
	case <-t.Context().Done():
		t.Fatal("test deadline reached before delivery eligibility")
	}
	require.NoError(t, b.DeliverBotIntent(t.Context(), key))
	current, err := botdelivery.Read(t.Context(), b.DB, b.Delivery.BotID, key, false)
	require.NoError(t, err)
	require.Positive(t, current.Attempt, "eligible delivery must reach the wire admission")
}

func testBotCaptureRequest(t *testing.T, requests <-chan []byte) []byte {
	t.Helper()
	deadline, ok := t.Deadline()
	require.True(t, ok, "the test uses the original Go test deadline")
	wait := time.NewTimer(max(time.Until(deadline)-time.Second, 0))
	defer wait.Stop()
	select {
	case body := <-requests:
		return body
	case <-wait.C:
		t.Fatal("no admitted HTTP request before the test deadline")
	case <-t.Context().Done():
		t.Fatal("test cancelled before an admitted HTTP request")
	}
	return nil
}

func TestBotTransportRetryCapturedDocument(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.UncertaintyRetryBase = time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	_, err := db.Exec(ctx, `UPDATE core.users SET telegram_id=101 WHERE id='alice';
 INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	original := bytes.Repeat([]byte("x"), telegram.MaxDocumentBytes)
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.fake_files(id,filename,body) VALUES('captured_original','original.bin',$1)`,
		original,
	)
	require.NoError(t, err)
	authorization := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer authorization.Close()
	b.API.Base, b.API.HTTP = authorization.URL, authorization.Client()
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	var sends, fileLookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			fileLookups.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/sendDocument") && sends.Add(1) == 1 {
			fake.Handler().ServeHTTP(httptest.NewRecorder(), r)
			connection, _, hijackErr := http.NewResponseController(w).Hijack()
			if hijackErr == nil {
				_ = connection.Close()
			}
			return
		}
		fake.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	queued, err := b.queueBotDocument(
		ctx,
		"alice",
		101,
		botdelivery.Reference{Family: "admin_file", Object: "captured_original",
			Update: 71, Continuation: botdelivery.Continuation{Kind: "document"}},
	)
	require.NoError(t, err)
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
	current, err := botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	wire, ref, err := botdelivery.RetryWire(ctx, db, current)
	require.NoError(t, err)
	require.Len(t, wire.Body, len(original))
	require.Equal(t, sha256.Sum256(original), sha256.Sum256(wire.Body))
	envelope, err := json.Marshal(map[string]any{
		"Observed": current, "Target": int64(0), "ExportEvents": []string(nil), "wire": ref,
	})
	require.NoError(t, err)
	require.Less(t, len(envelope), botdelivery.MaxRequestBytes, "20MiB body stays outside host JSON")
	_, err = db.Exec(
		ctx,
		`UPDATE bot.fake_files SET body=$1,filename='changed.bin' WHERE id='captured_original'`,
		[]byte("changed source"),
	)
	require.NoError(t, err)
	restarted := botDeliveryTestBot(db)
	restarted.Delivery, restarted.API, restarted.TG = b.Delivery, b.API, b.TG
	restarted.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	require.NoError(t, restarted.RecoverBotIntents(ctx))
	time.Sleep(time.Until(current.NotBefore) + 20*time.Millisecond)
	require.NoError(t, restarted.DeliverBotIntent(ctx, queued.Reference))
	require.EqualValues(t, 2, sends.Load())
	require.EqualValues(t, 1, fileLookups.Load(), "resend must not download a mutable source again")
	state := httptest.NewRecorder()
	fake.Handler().ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	var visible struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &visible))
	require.Len(t, visible.Messages, 2)
	for _, message := range visible.Messages {
		require.NotNil(t, message.Document)
		var filename string
		var body []byte
		require.NoError(
			t,
			db.QueryRow(ctx, `SELECT filename,body FROM bot.fake_files WHERE id=$1`, message.Document.FileID).
				Scan(&filename, &body),
		)
		require.Equal(t, wire.Filename, filename)
		require.Equal(t, sha256.Sum256(original), sha256.Sum256(body))
	}
}

func TestBotTransportRetryCapturePrivacyAndCorruption(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"history", "revoke", "missing", "corrupt"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			testBotCapturePrivateFault(t, fault)
		})
	}
}

func testBotCapturePrivateFault(t *testing.T, fault string) {
	t.Helper()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.UncertaintyRetryBase = time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	_, err := db.Exec(ctx, `UPDATE core.users SET telegram_id=101 WHERE id='alice'`)
	require.NoError(t, err)
	require.NoError(
		t,
		b.queueBotResult(ctx, "alice", 101, 72, "private-capture", botdelivery.Reference{Family: "static"},
			botdelivery.StoredResult{Payload: telegram.Send{Text: "private original"}}, 0),
	)
	operation, effect := botdelivery.ResultOperation("alice", 72, "private-capture")
	key := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	current, err := botdelivery.Read(ctx, db, 999, key, false)
	require.NoError(t, err)
	payload, err := telegram.PrepareSend(telegram.Send{ChatID: 101, Text: "private original"})
	require.NoError(t, err)
	storage := botdelivery.Service{DB: db, Delivery: b.Delivery}
	ref, err := storage.StageWire(ctx, current, botdelivery.Wire{Payload: payload})
	require.NoError(t, err)
	rendered := botRenderedDelivery{Payload: payload, Wire: ref}
	admitted, ready, err := b.beginBotIntent(ctx, current, rendered)
	require.NoError(t, err)
	require.True(t, ready)
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fake.Handler().ServeHTTP(httptest.NewRecorder(), r)
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	outcome, fallback := b.sendBotIntent(ctx, admitted, rendered)
	require.Equal(t, delivery.Uncertain, outcome.Kind)
	require.NoError(t, b.finishBotIntent(ctx, admitted, outcome, rendered.Receipt, fallback))
	window, err := b.API.ConversationWindow(ctx, "alice", 10)
	require.NoError(t, err)
	visibleHistory, err := json.Marshal(window)
	require.NoError(t, err)
	require.NotContains(t, string(visibleHistory), "private original", "wire storage is absent from model history")
	applyBotCaptureFault(t, &b, fault)
	testBotCaptureDeliverReady(t, &b, key)
	failed, err := botdelivery.Read(ctx, db, 999, key, false)
	require.NoError(t, err)
	expected := delivery.Rejected
	if fault == "history" || fault == "revoke" {
		expected = delivery.Cancelled
	}
	require.Equal(t, expected, failed.State)
	require.False(t, failed.ContinuationDone)
	require.Zero(t, failed.MessageID)
	require.EqualValues(t, 1, calls.Load(), "unavailable private source must not reconstruct or resend")
	var lastAttempt, resends int
	var reason string
	var confirmed *int64
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT last_uncertain_attempt,uncertain_resends,reason,last_confirmed_attempt FROM bot.delivery_intents
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`, b.Delivery.BotID, operation, effect).
			Scan(&lastAttempt, &resends, &reason, &confirmed),
	)
	require.Equal(t, 1, lastAttempt)
	require.Zero(t, resends)
	require.Nil(t, confirmed, "source and capture rejection are not a confirmed transport outcome")
	if expected == delivery.Rejected {
		require.Equal(t, "original_wire_unavailable", reason)
	}
}

func applyBotCaptureFault(t *testing.T, b *Bot, fault string) {
	t.Helper()
	var query string
	switch fault {
	case "history":
		query = `INSERT INTO core.conversation_history_generations(owner,generation) VALUES('alice',1)
 ON CONFLICT(owner) DO UPDATE SET generation=core.conversation_history_generations.generation+1`
	case "revoke":
		query = `UPDATE core.users SET telegram_id=102 WHERE id='alice'`
	case "missing":
		query = `DELETE FROM bot.interactions WHERE owner='alice' AND kind LIKE 'delivery_wire:%'`
	case "corrupt":
		query = `UPDATE bot.interactions SET content=jsonb_set(content,'{wire,payload,text}','"corrupt"')
 WHERE owner='alice' AND kind LIKE 'delivery_wire:%'`
	}
	_, err := b.DB.Exec(t.Context(), query)
	require.NoError(t, err)
	if fault == "history" {
		var bodies int
		require.NoError(t, b.DB.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions
 WHERE owner='alice' AND (kind LIKE 'delivery_wire:%' OR kind LIKE 'delivery_result:%')`).Scan(&bodies))
		require.Zero(t, bodies, "private results and staged/admitted wire disappear together")
	}
}

func TestBotTransportRetryDefaultBaseKeepsMissingCooldown(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.Fallback = 30 * time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	require.Zero(t, b.Delivery.UncertaintyRetryBase)
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			fake.Handler().ServeHTTP(httptest.NewRecorder(), r)
			connection, _, hijackErr := http.NewResponseController(w).Hijack()
			if hijackErr == nil {
				_ = connection.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	queued, err := b.enqueueBotIntent(ctx, "", 101, "default-cooldown", "notice", botdelivery.Reference{
		Kind: botdelivery.IdentityIntent, Update: 75, Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
	require.NoError(t, err)
	started := time.Now()
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
	current, err := botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	require.GreaterOrEqual(t, current.NotBefore.Sub(started), 5*time.Second)
	require.Less(
		t,
		current.NotBefore.Sub(started),
		10*time.Second,
		"missing-cooldown fallback is not the first uncertainty delay",
	)
	time.Sleep(time.Until(current.NotBefore) + 20*time.Millisecond)
	started = time.Now()
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
	current, err = botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	require.GreaterOrEqual(t, current.NotBefore.Sub(started), 30*time.Second)
	var global, chat time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT
 (SELECT not_before FROM core.delivery_pacing WHERE bot_id=999 AND chat=''),
 (SELECT not_before FROM core.delivery_pacing WHERE bot_id=999 AND chat='101')`).Scan(&global, &chat))
	require.GreaterOrEqual(t, global.Sub(started), 30*time.Second)
	require.GreaterOrEqual(t, chat.Sub(started), 30*time.Second)
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
	require.EqualValues(t, 2, calls.Load())
	var lastAttempt, resends int
	require.NoError(t, db.QueryRow(ctx, `SELECT last_uncertain_attempt,uncertain_resends FROM bot.delivery_intents
 WHERE operation_key='default-cooldown'`).Scan(&lastAttempt, &resends))
	require.Equal(t, 1, lastAttempt, "confirmed429 does not replace factual uncertainty")
	require.Equal(t, 1, resends)
}

func TestBotTransportRetryCaptureBindingAndStaging(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.UncertaintyRetryBase = time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	queued, err := b.enqueueBotIntent(ctx, "", 101, "capture-binding", "notice", botdelivery.Reference{
		Kind: botdelivery.IdentityIntent, Update: 76, Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
	require.NoError(t, err)
	current, err := botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	storage := botdelivery.Service{DB: db, Delivery: b.Delivery}
	payload, err := telegram.PrepareSend(telegram.Send{ChatID: 101, Text: "first candidate"})
	require.NoError(t, err)
	first, err := storage.StageWire(ctx, current, botdelivery.Wire{Payload: payload})
	require.NoError(t, err)
	payload.Text = "second candidate"
	second, err := storage.StageWire(ctx, current, botdelivery.Wire{Payload: payload})
	require.NoError(t, err)
	_, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{Payload: payload, Wire: first})
	require.ErrorIs(t, err, botdelivery.ErrWireUnavailable)
	require.False(t, ready)
	var attempt, resends int
	var capture *string
	require.NoError(t, db.QueryRow(ctx, `SELECT attempt,uncertain_resends,wire_capture_key FROM bot.delivery_intents
 WHERE operation_key='capture-binding'`).Scan(&attempt, &resends, &capture))
	require.Zero(t, attempt)
	require.Zero(t, resends)
	require.Nil(t, capture, "staging alone does not freeze an admitted original")
	wrong := current
	wrong.BotID++
	_, err = storage.StageWire(ctx, wrong, botdelivery.Wire{Payload: payload})
	require.ErrorIs(t, err, botdelivery.ErrBinding)
	wrong = current
	wrong.Owner = "alice"
	_, err = storage.StageWire(ctx, wrong, botdelivery.Wire{Payload: payload})
	require.ErrorIs(t, err, botdelivery.ErrBinding)
	admitted, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{Payload: payload, Wire: second})
	require.NoError(t, err)
	require.True(t, ready)
	wire, err := botdelivery.AdmittedWire(ctx, db, admitted)
	require.NoError(t, err)
	expectedWire, err := json.Marshal(payload)
	require.NoError(t, err)
	actualWire, err := json.Marshal(wire.Payload)
	require.NoError(t, err)
	require.JSONEq(t, string(expectedWire), string(actualWire))
	require.NoError(t, b.finishBotIntent(ctx, admitted,
		delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"}, wire.Receipt, false))
	current, err = botdelivery.Read(ctx, db, b.Delivery.BotID, queued.Reference, false)
	require.NoError(t, err)
	payload.Text = "must not replace original"
	_, err = storage.StageWire(ctx, current, botdelivery.Wire{Payload: payload})
	require.NoError(t, err)
	wire, _, err = botdelivery.RetryWire(ctx, db, current)
	require.NoError(t, err)
	require.Equal(t, "second candidate", wire.Payload.Text)
	wrong = admitted
	wrong.Attempt++
	_, err = botdelivery.AdmittedWire(ctx, db, wrong)
	require.ErrorIs(t, err, botdelivery.ErrWireUnavailable)
}

func TestBotTransportRetryLegacyUnknownCapturesNextWire(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.UncertaintyRetryBase = time.Second
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	queued, err := b.enqueueBotIntent(ctx, "", 101, "legacy-capture", "notice", botdelivery.Reference{
		Kind: botdelivery.IdentityIntent, Update: 77, Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
	require.NoError(t, err)
	current, err := botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	admitted, ready, err := b.beginBotIntent(ctx, current, botRenderedDelivery{})
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, b.finishBotIntent(
		ctx,
		admitted,
		delivery.Outcome{
			Kind:   delivery.Uncertain,
			Reason: "telegram_outcome_unknown",
		},
		botdelivery.Continuation{},
		false,
	))
	current, err = botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	_, capture, err := botdelivery.RetryWire(ctx, db, current)
	require.NoError(t, err)
	require.Nil(t, capture, "legacy uncertainty has no fabricated original capture")
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	server := httptest.NewServer(fake.Handler())
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	time.Sleep(time.Until(current.NotBefore) + 20*time.Millisecond)
	require.NoError(t, b.DeliverBotIntent(ctx, queued.Reference))
	current, err = botdelivery.Read(ctx, db, 999, queued.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, current.State)
	require.True(t, current.ContinuationDone)
	var originalUnknown, resends int
	var key, hash string
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT last_uncertain_attempt,uncertain_resends,wire_capture_key,wire_capture_hash
 FROM bot.delivery_intents WHERE operation_key='legacy-capture'`).Scan(&originalUnknown, &resends, &key, &hash),
	)
	require.Equal(t, 1, originalUnknown, "new capture does not invent historical delivery evidence")
	require.Equal(t, 1, resends)
	require.NotEmpty(t, key)
	require.Len(t, hash, sha256.Size*2)
}

func TestBotTransportRetryLostResponse(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Delivery.UncertaintyRetryBase = time.Second
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
	b.Delivery.UncertaintyRetryBase = time.Second
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
	assertBotTransportRetryTerminalReceipt(t, &b, first.Reference, rateLimited, visible.Messages, &calls)
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
	require.ErrorIs(t, b.finishBotIntent(ctx, attempt,
		telegram.DeliveryOutcome(0, &telegram.APIError{Code: http.StatusForbidden}), botdelivery.Continuation{}, false),
		botdelivery.ErrBinding, "a known positive receipt cannot be contradicted by a negative input")
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

func TestBotTransportRetryRecoveredKnownOutcomeFencesPositive(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"pending", "cancelled_after", "cancelled_before"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			ctx := t.Context()
			b := botDeliveryTestBot(db)
			b.Delivery.BotID = 999
			b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
			queued, err := b.enqueueBotIntent(ctx, "", 101, "recovered-known", "notice", botdelivery.Reference{
				Kind: botdelivery.IdentityIntent, Update: 1, Notice: i18n.IdentityUnavailable, Language: "en"}, "send")
			require.NoError(t, err)
			attempt := botRecoveredKnownHTTP(t, &b, queued.Reference, state)
			current, err := botdelivery.Read(ctx, db, 999, queued.Reference, false)
			require.NoError(t, err)
			require.Equal(t, attempt.Attempt, current.Attempt, "no new admission occurred")
			if state == "cancelled_after" {
				require.NoError(t, b.postponeBotIntent(ctx, current, true))
			}
			botAssertRecoveredKnownFence(t, &b, queued.Reference, attempt)
		})
	}
}

func botRecoveredKnownSnapshot(t *testing.T, b *Bot, ignored ...string) string {
	t.Helper()
	var raw string
	require.NoError(t, b.DB.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'intent',(SELECT to_jsonb(i)-COALESCE($1::text[],ARRAY[]::text[]) FROM bot.delivery_intents i WHERE bot_id=999 AND operation_key='recovered-known'),
 'queue',(SELECT jsonb_agg(to_jsonb(q)) FROM core.delivery_queue q WHERE bot_id=999),
 'pacing',(SELECT jsonb_agg(to_jsonb(p)) FROM core.delivery_pacing p WHERE bot_id=999))::text`, ignored).Scan(&raw))
	return raw
}

// Hold the actual HTTP request while recovery and optional cancellation complete.
func botRecoveredKnownHTTP(t *testing.T, b *Bot, ref delivery.Reference, state string) botdelivery.Intent {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w,
			`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`)
	}))
	defer server.Close()
	var released bool
	var completed bool
	completion := make(chan error, 1)
	defer func() {
		if !released {
			close(release)
		}
		cancel()
		if !completed {
			<-completion
		}
	}()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	go func() { completion <- b.DeliverBotIntent(ctx, ref) }()
	select {
	case <-entered:
	case err := <-completion:
		completed = true
		t.Fatalf("actual HTTP response did not reach recovery: %v", err)
	case <-t.Context().Done():
		t.Fatal("actual HTTP request did not arrive before test cancellation")
	}
	attempt, err := botdelivery.Read(t.Context(), b.DB, 999, ref, false)
	require.NoError(t, err, "actual HTTP response must have an admitted attempt")
	require.NoError(t, b.RecoverBotIntents(t.Context()))
	var before string
	if state == "cancelled_before" {
		recovered, readErr := botdelivery.Read(t.Context(), b.DB, 999, ref, false)
		require.NoError(t, readErr)
		require.NoError(t, b.postponeBotIntent(t.Context(), recovered, true))
		before = botRecoveredKnownSnapshot(t, b, "last_confirmed_attempt")
	}
	close(release)
	released = true
	completionErr := <-completion
	completed = true
	if completionErr != nil {
		t.Errorf("actual late confirmed429 must be persisted: %v", completionErr)
	}
	if state == "cancelled_before" {
		require.JSONEq(t, before, botRecoveredKnownSnapshot(t, b, "last_confirmed_attempt"),
			"terminal negative writes only the known-outcome fence")
	}
	return attempt
}

func botAssertRecoveredKnownFence(t *testing.T, b *Bot, ref delivery.Reference, attempt botdelivery.Intent) {
	t.Helper()
	var confirmed *int64
	var uncertain int64
	var resends int
	require.NoError(t, b.DB.QueryRow(t.Context(), `SELECT (to_jsonb(i)->>'last_confirmed_attempt')::bigint,
 last_uncertain_attempt,uncertain_resends FROM bot.delivery_intents i
 WHERE bot_id=999 AND operation_key='recovered-known'`).Scan(&confirmed, &uncertain, &resends))
	if confirmed == nil || *confirmed != attempt.Attempt {
		t.Error("the actual known response must durably fence its exact attempt")
	}
	require.Equal(t, attempt.Attempt, uncertain, "historical uncertainty remains factual")
	require.Zero(t, resends, "late completion does not admit a resend")
	before := botRecoveredKnownSnapshot(t, b)
	positiveErr := b.finishBotIntent(t.Context(), attempt,
		delivery.Outcome{Kind: delivery.Succeeded, MessageID: 900}, botdelivery.Continuation{}, false)
	if !errors.Is(positiveErr, botdelivery.ErrBinding) {
		t.Errorf("confirmed429 must fence contradictory same-attempt positive receipt: %v", positiveErr)
	}
	require.JSONEq(
		t,
		before,
		botRecoveredKnownSnapshot(t, b),
		"contradictory receipt cannot mutate owner, queue or pacing",
	)
	current, err := botdelivery.Read(t.Context(), b.DB, 999, ref, false)
	require.NoError(t, err)
	require.Zero(t, current.MessageID)
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
	b.Delivery.UncertaintyRetryBase = time.Second
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
