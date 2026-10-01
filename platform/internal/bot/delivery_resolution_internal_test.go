package bot

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestBotResolutionLostWireResponseUsesOriginalAttempt(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.BotID = 999
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	receipts := make(chan telegram.Message, 1)
	failures := make(chan error, 1)
	handler := fake.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var captured bool
		queryErr := db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM bot.delivery_attempts WHERE operation_key='lost')").
			Scan(&captured)
		if queryErr == nil && !captured {
			queryErr = errors.New("original attempt capture missing")
		}
		if queryErr != nil {
			failures <- queryErr
			http.Error(w, "capture missing", http.StatusInternalServerError)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		var envelope struct {
			Result telegram.Message `json:"result"`
		}
		if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &envelope); decodeErr != nil {
			failures <- decodeErr
			return
		}
		receipts <- envelope.Result
		// The real fake persisted the message; the client never receives its receipt.
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			failures <- hijackErr
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	ref := botdelivery.Reference{
		Kind:     botdelivery.IdentityIntent,
		Update:   1,
		Notice:   i18n.IdentityUnavailable,
		Language: "en",
	}
	_, err = b.enqueueBotIntent(ctx, "", 101, "lost", "notice", ref, "send")
	require.NoError(t, err)
	queueRef := delivery.Reference{Owner: delivery.Bot, Key: "lost", Effect: "notice"}
	require.NoError(t, b.DeliverBotIntent(ctx, queueRef))
	select {
	case failure := <-failures:
		require.NoError(t, failure)
	default:
	}
	var received telegram.Message
	select {
	case received = <-receipts:
	case <-time.After(5 * time.Second):
		t.Fatal("applied receipt missing")
	}
	message := received.ID
	require.Positive(t, message)
	require.Equal(t, b.Delivery.BotID, received.From.ID)
	s := botdelivery.Service{DB: db, Delivery: b.Delivery}
	_, err = db.Exec(ctx, "INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING")
	require.NoError(t, err)
	key := botdelivery.IntentKey{Operation: "lost", Effect: "notice"}
	before, err := s.Inspect(ctx, "alice", key)
	require.NoError(t, err)
	require.Equal(t, delivery.Uncertain, before.State)
	result, err := s.Resolve(
		ctx,
		"alice",
		botdelivery.Resolution{
			IntentKey:      key,
			Key:            "known-receipt",
			Attempt:        before.Attempt,
			Disposition:    "confirmed_sent",
			EvidenceKind:   "provider_receipt",
			EvidenceSHA256: strings.Repeat("a", 64),
			PayloadSHA256:  before.PayloadSHA256,
			MessageID:      message,
			Joined:         true,
			Quiescent:      true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, result.State)
	// A reconstructed Bot completes the original receipt without any HTTP call.
	restarted := botDeliveryTestBot(db)
	restarted.Delivery = b.Delivery
	restarted.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	require.Equal(t, b.Delivery.BotID, restarted.Delivery.BotID)
	t.Logf(
		"original_bot_id=%d host_bot_id=%d reconstructed_bot_id=%d provider_bot_id=%d",
		b.Delivery.BotID,
		b.Host.LocalBotDelivery.Service.Delivery.BotID,
		restarted.Delivery.BotID,
		received.From.ID,
	)
	require.NoError(t, restarted.ContinueBotIntentReceipts(ctx))
	var completed bool
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT continuation_done FROM bot.delivery_intents WHERE operation_key='lost'").
			Scan(&completed),
	)
	require.True(t, completed)
	require.Empty(t, receipts)
	// The Telegram-like UI reads the same single persisted message after restart.
	reloaded, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	state := httptest.NewRecorder()
	reloaded.Handler().ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	require.Equal(t, http.StatusOK, state.Code)
	var visible struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &visible))
	require.Len(t, visible.Messages, 1)
	require.Equal(t, message, visible.Messages[0].ID)
}

func TestBotAttemptCorrelationIncludesWireTargetAndContent(t *testing.T) {
	t.Parallel()
	i := botdelivery.Intent{
		Chat:      101,
		Phase:     botPhaseEdit,
		Target:    8,
		Reference: botdelivery.Reference{Kind: botdelivery.CardIntent},
	}
	r := botRenderedDelivery{Payload: telegram.Send{MessageID: 8, Text: "original private response"}}
	original, err := prepareBotAttempt(i, r)
	require.NoError(t, err)
	require.Equal(t, "editMessageText", original.Method)
	r.Payload.MessageID = 9
	target, err := prepareBotAttempt(i, r)
	require.NoError(t, err)
	require.NotEqual(t, original.SHA256, target.SHA256)
	r.Payload.MessageID = 8
	r.Payload.Text = "later response"
	content, err := prepareBotAttempt(i, r)
	require.NoError(t, err)
	require.NotEqual(t, original.SHA256, content.SHA256)
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private response")
}

func TestBotDocumentCorrelationUsesOnlyLogicalWireFields(t *testing.T) {
	t.Parallel()
	i := botdelivery.Intent{Chat: 101, Phase: botDocumentKind}
	r := botRenderedDelivery{
		Filename: "receipt.txt",
		Body:     []byte("original receipt"),
		Payload:  telegram.Send{Text: "unused"},
	}
	original, err := prepareBotAttempt(i, r)
	require.NoError(t, err)
	require.Equal(t, "sendDocument", original.Method)
	r.Payload.Text = "not sent with a document"
	same, err := prepareBotAttempt(i, r)
	require.NoError(t, err)
	require.Equal(t, original.SHA256, same.SHA256)
	r.Body = []byte("changed receipt")
	changed, err := prepareBotAttempt(i, r)
	require.NoError(t, err)
	require.NotEqual(t, original.SHA256, changed.SHA256)
}

func TestBotResolutionUnsentRestoresOrderedRealDelivery(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	ctx := t.Context()
	b := botDeliveryTestBot(db)
	b.Delivery.Fallback = time.Microsecond
	b.Delivery.BotID = 999
	b.Host.LocalBotDelivery.Service.Delivery = b.Delivery
	fake, err := sandbox.New(ctx, db, "synthetic")
	require.NoError(t, err)
	server := httptest.NewServer(fake.Handler())
	defer server.Close()
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	ref := botdelivery.Reference{
		Kind:     botdelivery.IdentityIntent,
		Update:   1,
		Notice:   i18n.IdentityUnavailable,
		Language: "en",
	}
	first, err := b.enqueueBotIntent(ctx, "", 101, "unsent", "notice", ref, "send")
	require.NoError(t, err)
	i, err := botdelivery.Read(ctx, db, b.Delivery.BotID, first.Reference, false)
	require.NoError(t, err)
	rendered, err := b.renderBotIntent(ctx, i)
	require.NoError(t, err)
	rendered.Payload, err = telegram.PrepareSend(rendered.Payload)
	require.NoError(t, err)
	attempt, ready, err := b.beginBotIntent(ctx, i, rendered)
	require.NoError(t, err)
	require.True(t, ready)
	// This joined sender never called the provider after durable admission.
	require.NoError(
		t,
		b.finishBotIntent(
			ctx,
			attempt,
			delivery.Outcome{Kind: delivery.Uncertain, Reason: "delivery_interrupted"},
			rendered.Receipt,
			false,
		),
	)
	ref.Update = 2
	second, err := b.enqueueBotIntent(ctx, "", 101, "follower", "notice", ref, "send")
	require.NoError(t, err)
	require.NoError(t, b.DeliverBotIntent(ctx, second.Reference))
	follower, err := botdelivery.Read(ctx, db, b.Delivery.BotID, second.Reference, false)
	require.NoError(t, err)
	require.Zero(t, follower.Attempt)
	_, err = db.Exec(ctx, "INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING")
	require.NoError(t, err)
	s := botdelivery.Service{DB: db, Delivery: b.Delivery}
	key := botdelivery.IntentKey{Operation: "unsent", Effect: "notice"}
	before, err := s.Inspect(ctx, "alice", key)
	require.NoError(t, err)
	_, err = s.Resolve(
		ctx,
		"alice",
		botdelivery.Resolution{
			IntentKey:      key,
			Key:            "unsent-proof",
			Attempt:        before.Attempt,
			Disposition:    "confirmed_unsent",
			EvidenceKind:   "pre_dispatch_failure",
			EvidenceSHA256: strings.Repeat("a", 64),
			PayloadSHA256:  before.PayloadSHA256,
			Joined:         true,
			Quiescent:      true,
		},
	)
	require.NoError(t, err)
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		assert.NoError(check, b.DeliverBotIntent(ctx, first.Reference))
		current, readErr := botdelivery.Read(ctx, db, b.Delivery.BotID, first.Reference, false)
		assert.NoError(check, readErr)
		assert.Equal(check, delivery.Succeeded, current.State)
	}, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, b.DeliverBotIntent(ctx, second.Reference))
	firstSent, err := botdelivery.Read(ctx, db, b.Delivery.BotID, first.Reference, false)
	require.NoError(t, err)
	followerSent, err := botdelivery.Read(ctx, db, b.Delivery.BotID, second.Reference, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, firstSent.State)
	require.Equal(t, int64(2), firstSent.Attempt)
	require.Equal(t, delivery.Succeeded, followerSent.State)
	require.Less(t, firstSent.MessageID, followerSent.MessageID)
	t.Logf(
		"original_bot_id=%d host_bot_id=%d resolved_bot_id=%d",
		b.Delivery.BotID,
		b.Host.LocalBotDelivery.Service.Delivery.BotID,
		before.BotID,
	)
	state := httptest.NewRecorder()
	fake.Handler().ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/lab/state?user=101", nil))
	require.Equal(t, http.StatusOK, state.Code)
	var visible struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(state.Body.Bytes(), &visible))
	require.Len(t, visible.Messages, 2)
}
