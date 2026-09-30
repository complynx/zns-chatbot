package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// The same ambiguous response can mean either no wire effect or a committed
// edit. Neither observation authorizes a new send or overtaking the lane head.
type uncertainMediaEditTransport struct {
	mu         sync.Mutex
	target     int64
	afterApply bool
	edits      []int64
	sends      int
	applied    int
}

func (tr *uncertainMediaEditTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/sendMessage") {
		tr.mu.Lock()
		tr.sends++
		tr.mu.Unlock()
	}
	if !strings.HasSuffix(req.URL.Path, "/editMessageText") {
		return http.DefaultTransport.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	var send telegram.Send
	if err = json.Unmarshal(body, &send); err != nil {
		return nil, err
	}
	tr.mu.Lock()
	tr.edits = append(tr.edits, send.MessageID)
	tr.mu.Unlock()
	if send.MessageID != tr.target {
		return http.DefaultTransport.RoundTrip(req)
	}
	if tr.afterApply {
		response, wireErr := http.DefaultTransport.RoundTrip(req)
		if wireErr != nil {
			return nil, wireErr
		}
		_, err = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, err
		}
		tr.mu.Lock()
		tr.applied++
		tr.mu.Unlock()
	}
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
		Body:    io.NopCloser(strings.NewReader(`{"ok":false,"error_code":503,"description":"ambiguous edit"}`)),
		Request: req}, nil
}

func (tr *uncertainMediaEditTransport) assertAttempts(t *testing.T, applied int) {
	t.Helper()
	tr.mu.Lock()
	defer tr.mu.Unlock()
	assert.Equal(t, []int64{tr.target}, tr.edits, "only the first known card may reach the wire")
	assert.Zero(t, tr.sends, "ambiguous edit must never become a new send")
	assert.Equal(t, applied, tr.applied)
}

func pendingMediaEdit(t *testing.T, f *fixture, target int64) botdelivery.Intent {
	t.Helper()
	ref := delivery.Reference{Owner: delivery.Bot}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key
 FROM bot.delivery_intents WHERE bot_id=$1 AND owner='alice' AND chat_id=101
 AND target_message_id=$2 AND phase='edit' AND state='pending'`, f.b.Delivery.BotID, target).
		Scan(&ref.Key, &ref.Effect))
	intent, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.Equal(t, "alice", intent.Owner)
	require.EqualValues(t, 101, intent.Chat)
	require.Equal(t, target, intent.Target)
	return intent
}

func assertUncertainMediaLane(t *testing.T, f *fixture, first, second botdelivery.Intent) {
	t.Helper()
	for _, item := range []struct {
		intent   botdelivery.Intent
		state    delivery.Kind
		attempts int64
	}{
		{first, delivery.Uncertain, 1}, {second, delivery.Deferred, 0},
	} {
		current, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, item.intent.QueueReference(), false)
		require.NoError(t, err)
		assert.Equal(t, item.state, current.State)
		assert.Equal(t, item.attempts, current.Attempt)
		assert.Equal(t, item.intent.Target, current.Target)
		assert.Equal(t, item.intent.Reference, current.Reference)
		assert.Equal(t, "edit", current.Phase)
		assert.Zero(t, current.MessageID, "unknown wire outcome must not acquire a success receipt")
		assert.False(t, current.ContinuationDone)
		var state string
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state FROM core.delivery_queue
 WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key=$4`,
			f.b.Delivery.BotID, delivery.Bot, current.Operation, current.Effect).Scan(&state))
		assert.Equal(t, string(item.state), state)
	}
	for _, entry := range botDeliveryCandidates(t, f.b) {
		assert.NotEqual(t, first.QueueReference(), entry.Reference)
		assert.NotEqual(t, second.QueueReference(), entry.Reference)
	}
}

func TestMediaAmbiguousEditRetainsSameChatBarrierAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, afterApply := range []bool{false, true} {
		name := "before_sink"
		if afterApply {
			name = "after_sink_response_lost"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
			require.NoError(t, err)
			order := intakeOrder(t, f, "first")
			photo, original := intakePhoto(t, f)
			f.model.plan = terminalReceiptPlan()
			handleVisible(t, f.b, photo)
			committed, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			require.Equal(t, "proof", committed.State)
			require.Equal(t, order.Version+1, committed.Version)
			firstCard := mediaReconcileCard(t, f, "tg-media-100")
			require.Contains(t, firstCard.Text, "Receipt submitted for review.")
			photo.ID = 101
			f.model.plan = agent.Plan{View: agent.MediaView, Text: "What is this photo?"}
			handleVisible(t, f.b, photo)
			secondCard := mediaReconcileCard(t, f, "tg-media-101")
			require.NotEqual(t, firstCard.ID, secondCard.ID)
			require.NotEmpty(t, secondCard.Markup.Rows)
			messages := len(chatMessages(t, f, 101))
			modelCalls := f.model.calls

			// A legitimate language change requests two replacements of known IDs.
			// No expiry or SQL state manipulation is needed to create the edit attempts.
			_, err = f.b.API.SetLanguage(t.Context(), "alice", "ru", false)
			require.NoError(t, err)
			require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, "tg-media-100"))
			require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, "tg-media-101"))
			first := pendingMediaEdit(t, f, firstCard.ID)
			second := pendingMediaEdit(t, f, secondCard.ID)
			tr := &uncertainMediaEditTransport{target: firstCard.ID, afterApply: afterApply}
			f.b.TG.HTTP = &http.Client{Transport: tr}
			pumpBotDeliveries(t, f.b)
			assertUncertainMediaLane(t, f, first, second)
			applied := 0
			if afterApply {
				applied = 1
			}
			tr.assertAttempts(t, applied)
			firstVisible := mediaReconcileCard(t, f, "tg-media-100")
			if afterApply {
				assert.NotEqual(t, firstCard.Text, firstVisible.Text)
				assert.Contains(t, firstVisible.Text, "Вложение")
			} else {
				assert.Equal(t, firstCard.Text, firstVisible.Text)
			}
			assert.Equal(t, secondCard, mediaReconcileCard(t, f, "tg-media-101"))

			restartedModel := restartOrderBot(f)
			require.NoError(t, f.b.RecoverBotIntents(t.Context()))
			require.NoError(t, f.b.ContinueBotIntentReceipts(t.Context()))
			pumpBotDeliveries(t, f.b)
			// Even explicit owner dispatch cannot bypass the shared lane's unknown head.
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), first.QueueReference()))
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), second.QueueReference()))
			assertUncertainMediaLane(t, f, first, second)
			tr.assertAttempts(t, applied)
			assert.Equal(t, firstVisible, mediaReconcileCard(t, f, "tg-media-100"))
			assert.Equal(t, secondCard, mediaReconcileCard(t, f, "tg-media-101"))
			assert.Len(t, chatMessages(t, f, 101), messages)
			assert.Zero(t, restartedModel.calls)
			assert.Equal(t, modelCalls, f.model.calls)
			current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, committed, current, "delivery recovery must not repeat the payment operation")
			proof, err := (orders.Service{DB: f.db}).OrderProof(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, original, proof.Body)
			t.Log("Ambiguous known-ID edit remains unknown; same-chat replacement waits for supported resolution.")
		})
	}
}
