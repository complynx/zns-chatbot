package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestPaymentInstructionsReadOnlyAndOwnerScoped(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "instructions",
		Choice:  orderChoice("preparty"),
	})
	require.NoError(t, err)
	info, err := f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, orders.Money(3500), info.TotalBYN)
	assert.Equal(t, "1050.00", info.TotalRUB)
	assert.True(t, info.CanPay)
	assert.Contains(t, info.Transfer, "ТЕСТОВЫЕ")
	require.Len(t, info.Contacts, 1)
	assert.EqualValues(t, 202, info.Contacts[0].TelegramID)
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order.Version, current.Version)
	_, err = f.b.API.PaymentInstructions(t.Context(), "bob", order.EventID, order.ID)
	require.Error(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.order_events SET deadline=now()-interval '1 day' WHERE id=$1`,
		order.EventID,
	)
	require.NoError(t, err)
	info, err = f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.False(t, info.CanPay)
	assert.Empty(t, info.Transfer)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	_, err = f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.Error(t, err)
}

func TestPaymentInstructionsLocalizedContentFallback(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ tag, want string }{
		{"by", "Russian instructions"}, {"ua", "Russian instructions"},
		{"be-BY", "Russian instructions"}, {"uk-UA", "Exact Ukrainian instructions"},
		{"pl", "English instructions"}, {"pl-PL", "English instructions"},
	} {
		t.Run(test.tag, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "fallback",
				Choice: orderChoice("preparty"),
			})
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET transfer_instructions_localized=
				'{"ru":"Russian instructions","en":"English instructions","uk-UA":"Exact Ukrainian instructions"}' WHERE id=$1`, order.EventID)
			require.NoError(t, err)
			_, resetErr := f.db.Exec(t.Context(), `UPDATE core.users SET language='' WHERE id='alice'`)
			require.NoError(t, resetErr)
			_, setErr := f.b.API.SetLanguage(t.Context(), "alice", test.tag, true)
			require.NoError(t, setErr)
			info, readErr := f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, readErr)
			assert.Equal(t, test.want, info.Transfer)
		})
	}
}

func TestDeletedPaymentCardKeepsSelectedLanguage(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			testDeletedPaymentCard(t, language)
		})
	}
}

func testDeletedPaymentCard(t *testing.T, language string) {
	t.Helper()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "retire",
		Choice: orderChoice("preparty"),
	})
	require.NoError(t, err)
	handleVisible(t, f.b, message(900, 101, "/language en"))
	handleVisible(t, f.b, message(901, 101, "/orders"))
	handleVisible(t, f.b, orderClick(t, f, 101, 902, "Payment methods"))
	opened := paymentMessage(t, f)
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-retired",
	})
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	preference, err := f.b.API.SetLanguage(t.Context(), "alice", language, false)
	require.NoError(t, err)
	require.Equal(t, language, preference.Language)
	pumpBotDeliveries(t, f.b)
	want, err := i18n.Translate(language, i18n.PaymentUnavailable,
		map[string]string{"code": "payment_context_unavailable"})
	require.NoError(t, err)
	var found bool
	for _, item := range chatMessages(t, f, 101) {
		if item.ID == opened.ID {
			found = true
			assert.Equal(t, want, item.Text)
			assert.NotContains(t, item.Text, "TEST PAYMENT DETAILS")
			assert.Empty(t, item.Markup.Rows)
		}
	}
	assert.True(t, found)
	var projectedID int64
	var visible bool
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT message_id,visible FROM bot.order_cards WHERE owner='alice' AND card_key=$1`, "payment:"+order.ID).
		Scan(&projectedID, &visible))
	assert.Equal(t, opened.ID, projectedID)
	assert.False(t, visible)
}

func TestPaymentRetirementRejectsForeignOrUnboundTargets(t *testing.T) {
	t.Parallel()
	f, order, opened := openedPaymentFixture(t)
	generation := int64(0)
	request := paymentRetirementRequest(t, f, order, opened)
	ref := request.Reference
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-forged-notice",
	})
	require.NoError(t, err)
	request.Target = 0
	require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "retirement cannot send a new message")
	request.Target = opened.ID + 10000
	require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "retirement cannot edit another message")
	request.Target, request.Owner, request.Chat = opened.ID, "bob", 202
	require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "retirement cannot adopt another owner's card")
	request.Owner, request.Chat = "alice", 101
	request.Reference.Source = &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "retirement cannot carry private source content")
	request.Reference = ref
	request.Reference.CardKey = "menu"
	require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "retirement cannot adopt a nonpayment card")
	request.Reference = ref
	request.Reference.Notice = i18n.OrderOffPage
	require.Error(
		t,
		f.b.Host.EnqueueBotCard(t.Context(), request),
		"another notice cannot grant source-free retirement",
	)
}

func TestPaymentRetirementMissingEditNeverSends(t *testing.T) {
	t.Parallel()
	f, order, opened := openedPaymentFixture(t)
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-no-fallback",
	})
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	intent := queuedPaymentRetirement(t, f)
	var edits, sends atomic.Int64
	var observedTarget atomic.Int64
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits.Add(1)
			var payload telegram.Send
			if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr != nil {
				return nil, decodeErr
			}
			observedTarget.Store(payload.MessageID)
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Request:    r,
				Body: io.NopCloser(
					strings.NewReader(
						`{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`,
					),
				),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	assert.EqualValues(t, 1, edits.Load())
	assert.Equal(t, opened.ID, observedTarget.Load())
	assert.Zero(t, sends.Load())
	current, err := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
	require.NoError(t, err)
	assert.Equal(t, delivery.Rejected, current.State)
	var reason string
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT reason FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID, current.Operation, current.Effect).Scan(&reason))
	assert.Equal(t, "edit_target_missing", reason)
	assert.EqualValues(t, 1, current.Attempt)
	assert.Zero(t, current.MessageID)
}

func TestPaymentRetirementRejectsLiveOrder(t *testing.T) {
	t.Parallel()
	f, order, opened := openedPaymentFixture(t)
	ref := paymentRetirementRequest(t, f, order, opened)
	require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), ref))
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'notice'=$1`, string(i18n.PaymentUnavailable)).Scan(&count))
	assert.Zero(t, count)
	assert.Equal(t, opened.Text, paymentMessage(t, f).Text)
}

func paymentRetirementRequest(
	t *testing.T,
	f *fixture,
	order orders.Order,
	opened telegram.Message,
) botdelivery.CardRequest {
	t.Helper()
	prior := &botdelivery.PaymentRetirement{}
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key,receipt->>'view_hash'
 FROM bot.delivery_intents WHERE owner='alice' AND state='sent' AND message_id=$1
 AND reference->>'card_key'=$2 ORDER BY attempted_at DESC,created_at DESC LIMIT 1`,
		opened.ID, "payment:"+order.ID).Scan(&prior.Operation, &prior.Effect, &prior.ViewHash))
	generation := int64(0)
	return botdelivery.CardRequest{
		Owner: "alice", Chat: 101, Target: opened.ID,
		Reference: botdelivery.Reference{
			Kind: botdelivery.CardIntent, Family: "payment", CardKey: "payment:" + order.ID,
			Event: order.EventID, Object: order.ID, Notice: i18n.PaymentUnavailable, Generation: &generation,
			PaymentRetirement: prior,
			Continuation:      botdelivery.Continuation{Kind: "order_card", Key: "payment:" + order.ID, Retired: true},
		},
	}
}

func TestPaymentRetirementPreservesOtherEventLiveCard(t *testing.T) {
	t.Parallel()
	f, order, opened := openedPaymentFixture(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.order_events(id,deadline,menu,extras)
 SELECT 'second-event',deadline,menu,extras FROM core.order_events WHERE id='sandbox-festival'`)
	require.NoError(t, err)
	scoped := *f.b
	scoped.OrderEventID = "second-event"
	require.NoError(t, scoped.RenderOrders(t.Context(), "alice", 101))
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'card_key'=$1 AND reference->>'notice'=$2`,
		"payment:"+order.ID, string(i18n.PaymentUnavailable)).Scan(&count))
	assert.Zero(t, count)
	current := paymentMessage(t, f)
	assert.Equal(t, opened, current)
	var visible bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT visible FROM bot.order_cards
 WHERE owner='alice' AND card_key=$1`, "payment:"+order.ID).Scan(&visible))
	assert.True(t, visible)
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-other-event-card",
	})
	require.NoError(t, err)
	require.NoError(t, scoped.RenderOrders(t.Context(), "alice", 101))
	intent := queuedPaymentRetirement(t, f)
	assert.Equal(t, order.EventID, intent.Reference.Event)
	require.NoError(t, scoped.DeliverBotIntent(t.Context(), intent.QueueReference()))
	var found bool
	for _, item := range chatMessages(t, f, 101) {
		if item.ID == opened.ID {
			current, found = item, true
		}
	}
	require.True(t, found)
	want, translationErr := i18n.Translate("en", i18n.PaymentUnavailable,
		map[string]string{"code": "payment_context_unavailable"})
	require.NoError(t, translationErr)
	assert.Equal(t, opened.ID, current.ID)
	assert.Equal(t, want, current.Text)
	assert.Empty(t, current.Markup.Rows)
	completed, readErr := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
	require.NoError(t, readErr)
	assert.Equal(t, delivery.Succeeded, completed.State)
	assert.True(t, completed.ContinuationDone)
	assertPaymentRetirementQueue(t, f, completed, string(delivery.Succeeded))
}

func TestPaymentRetirementPreservesSameMessageProjection(t *testing.T) {
	t.Parallel()
	for _, afterSend := range []bool{false, true} {
		for _, source := range []bool{false, true} {
			name := "hash_before_admission"
			if source {
				name = "source_before_admission"
			}
			if afterSend {
				name = strings.ReplaceAll(name, "admission", "receipt")
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				testPaymentRetirementSameMessage(t, afterSend, source)
			})
		}
	}
}

func testPaymentRetirementSameMessage(t *testing.T, afterSend, source bool) {
	t.Helper()
	f, order, opened := openedPaymentFixture(t)
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-same-message",
	})
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	intent := queuedPaymentRetirement(t, f)
	var oldHash string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT view_hash FROM bot.order_cards
 WHERE owner='alice' AND card_key=$1`, "payment:"+order.ID).Scan(&oldHash))
	wantHash := "newer-payment-projection"
	if source {
		wantHash = oldHash
	}
	replace := func() error {
		if source {
			_, updateErr := f.db.Exec(t.Context(), `UPDATE bot.interactions
 SET content='{"original":false,"source":{"generation":0,"authorities":[]}}'::jsonb
 WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID)
			return updateErr
		}
		_, updateErr := f.db.Exec(t.Context(), `UPDATE bot.order_cards SET view_hash=$1,visible=true
 WHERE owner='alice' AND card_key=$2`, wantHash, "payment:"+order.ID)
		return updateErr
	}
	var replaced atomic.Bool
	if afterSend {
		f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
			if r.URL.Path == "/internal/bot-delivery/receipt" && !replaced.Swap(true) {
				return replace()
			}
			return nil
		}}}
	} else {
		require.NoError(t, replace())
	}
	var wire atomic.Int64
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		wire.Add(1)
		return http.DefaultTransport.RoundTrip(r)
	})}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	current, readErr := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
	require.NoError(t, readErr)
	queueState := string(delivery.Cancelled)
	if afterSend {
		assert.True(t, replaced.Load())
		assert.EqualValues(t, 1, wire.Load())
		assert.Equal(t, delivery.Succeeded, current.State)
		assert.True(t, current.ContinuationDone)
		assert.EqualValues(t, 1, current.Attempt)
		queueState = string(delivery.Succeeded)
	} else {
		assert.Zero(t, wire.Load())
		assert.Equal(t, delivery.Cancelled, current.State)
		assert.False(t, current.ContinuationDone)
		assert.Zero(t, current.Attempt)
	}
	assertPaymentRetirementQueue(t, f, current, queueState)
	var target int64
	var hash string
	var visible bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT message_id,view_hash,visible FROM bot.order_cards
 WHERE owner='alice' AND card_key=$1`, "payment:"+order.ID).Scan(&target, &hash, &visible))
	assert.Equal(t, opened.ID, target)
	assert.Equal(t, wantHash, hash)
	assert.True(t, visible)
	if source {
		var raw []byte
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID).Scan(&raw))
		assert.JSONEq(t, `{"original":false,"source":{"generation":0,"authorities":[]}}`, string(raw))
	}
}

func assertPaymentRetirementQueue(t *testing.T, f *fixture, current botdelivery.Intent, want string) {
	t.Helper()
	var state string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state FROM core.delivery_queue
 WHERE bot_id=$1 AND owner_kind='bot' AND owner_key=$2 AND effect_key=$3`,
		current.BotID, current.Operation, current.Effect).Scan(&state))
	assert.Equal(t, want, state)
}

func TestOffPagePaymentKeepsLiveContentAndOriginalSource(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPagingOrders(t, f, false)
	handleVisible(t, f.b, message(930, 101, "/language ru"))
	handleVisible(t, f.b, message(931, 101, "/orders"))
	f.model.plan = agent.Plan{
		View: agent.OrdersView, OrderAction: &agent.OrderProposal{
			Name: orders.ActionPaymentInstructions, OrderID: "page-0001",
		},
	}
	handleVisible(t, f.b, message(932, 101, "Show payment instructions for page-0001"))
	opened := paymentMessage(t, f)
	require.Contains(t, opened.Text, "ТЕСТОВЫЕ РЕКВИЗИТЫ")
	var source []byte
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT content FROM bot.interactions WHERE owner='alice' AND kind='payment_source:page-0001'`).Scan(&source))
	handleVisible(t, f.b, pageClick(t, f, 101, 933, "orders", true))
	current := paymentMessage(t, f)
	assert.Equal(t, opened.ID, current.ID)
	assert.Equal(t, opened.Text, current.Text)
	var retained []byte
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT content FROM bot.interactions WHERE owner='alice' AND kind='payment_source:page-0001'`).Scan(&retained))
	assert.JSONEq(t, string(source), string(retained))
	orderDeliveryHistoryDelete(t, f, "alice")
	require.Error(t, f.b.RenderOrders(t.Context(), "alice", 101), "off-page content retains its original source guards")
}

func TestPaymentRetirementDoesNotReplaceNewerCard(t *testing.T) {
	t.Parallel()
	for _, afterSend := range []bool{false, true} {
		name := "before_admission"
		if afterSend {
			name = "before_receipt"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testPaymentRetirementNewerCard(t, afterSend)
		})
	}
}

func testPaymentRetirementNewerCard(t *testing.T, afterSend bool) {
	t.Helper()
	f, order, opened := openedPaymentFixture(t)
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-newer-card",
	})
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	intent := queuedPaymentRetirement(t, f)
	replace := func() error {
		_, updateErr := f.db.Exec(
			t.Context(),
			`UPDATE bot.order_cards SET message_id=$1 WHERE owner='alice' AND card_key=$2`,
			opened.ID+10000,
			"payment:"+order.ID,
		)
		return updateErr
	}
	var replaced atomic.Bool
	if afterSend {
		f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
			if r.URL.Path == "/internal/bot-delivery/receipt" && !replaced.Swap(true) {
				return replace()
			}
			return nil
		}}}
	} else {
		require.NoError(t, replace())
	}
	var wire atomic.Int64
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		wire.Add(1)
		return http.DefaultTransport.RoundTrip(r)
	})}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	if afterSend {
		assert.True(t, replaced.Load())
		assert.EqualValues(t, 1, wire.Load())
	} else {
		assert.Zero(t, wire.Load())
	}
	var target int64
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		`SELECT message_id FROM bot.order_cards WHERE owner='alice' AND card_key=$1`,
		"payment:"+order.ID,
	).Scan(&target))
	assert.Equal(t, opened.ID+10000, target)
}

func openedPaymentFixture(t *testing.T) (*fixture, orders.Order, telegram.Message) {
	t.Helper()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "retire-bound",
		Choice: orderChoice("preparty"),
	})
	require.NoError(t, err)
	handleVisible(t, f.b, message(900, 101, "/language en"))
	handleVisible(t, f.b, message(901, 101, "/orders"))
	handleVisible(t, f.b, orderClick(t, f, 101, 902, "Payment methods"))
	return f, order, paymentMessage(t, f)
}

func queuedPaymentRetirement(t *testing.T, f *fixture) botdelivery.Intent {
	t.Helper()
	var operation, effect string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
		WHERE owner='alice' AND reference->>'family'='payment' AND (reference->'continuation'->>'retired')::boolean
		ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
	intent, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID,
		delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}, false)
	require.NoError(t, err)
	for range 100 {
		for _, entry := range botDeliveryCandidates(t, f.b) {
			if entry.Reference == intent.QueueReference() {
				return intent
			}
			if entry.Reference.Owner == delivery.Bot {
				require.NoError(t, f.b.DeliverBotIntent(t.Context(), entry.Reference))
				break
			}
		}
		time.Sleep(max(f.b.Delivery.BotInterval, f.b.Delivery.ChatInterval) + time.Millisecond)
	}
	t.Fatal("payment retirement did not become the lane head")
	return botdelivery.Intent{}
}

func TestPaymentUnavailableContextKeepsExactTarget(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, refusal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/refusal_%t", language, refusal), func(t *testing.T) {
				t.Parallel()
				f, order, opened := openedPaymentFixture(t)
				_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
				require.NoError(t, err)
				require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
				intent := queuedPaymentRetirement(t, f)
				_, err = f.b.API.SetLanguage(t.Context(), "alice", language, false)
				require.NoError(t, err)
				assertUnavailablePaymentDelivery(t, f, order, opened, intent, language, refusal)
			})
		}
	}

	runPaymentRetirementHistoryCases(t)
}

func assertUnavailablePaymentDelivery(
	t *testing.T, f *fixture, order orders.Order, opened telegram.Message,
	intent botdelivery.Intent, language string, refusal bool,
) {
	t.Helper()
	var edits, sends atomic.Int64
	observed := make(chan telegram.Send, 1)
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits.Add(1)
			var payload telegram.Send
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			select {
			case observed <- payload:
			default:
				return nil, fmt.Errorf("duplicate payment edit")
			}
			if refusal {
				return paymentEditRefused(r), nil
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	require.EqualValues(t, 1, edits.Load())
	require.Zero(t, sends.Load())
	payload := <-observed
	want, err := i18n.Translate(language, i18n.PaymentUnavailable,
		map[string]string{"code": "payment_context_unavailable"})
	require.NoError(t, err)
	require.Equal(t, opened.ID, payload.MessageID)
	require.EqualValues(t, 101, payload.ChatID)
	require.Equal(t, want, payload.Text)
	require.Empty(t, payload.Markup.Rows)
	assertUnavailablePaymentReceipt(t, f, order, opened, intent, refusal)
	if !refusal {
		require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
		var count int
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'card_key'=$1 AND reference->>'notice'=$2`,
			"payment:"+order.ID, string(i18n.PaymentUnavailable)).Scan(&count))
		require.Equal(t, 1, count, "repeated refresh cannot recreate a retired payment card")
	}
}

func paymentEditRefused(r *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest, Header: make(http.Header), Request: r,
		Body: io.NopCloser(strings.NewReader(
			`{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`)),
	}
}

func assertUnavailablePaymentReceipt(
	t *testing.T, f *fixture, order orders.Order, opened telegram.Message, intent botdelivery.Intent, refusal bool,
) {
	t.Helper()
	current, err := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
	require.NoError(t, err)
	require.Nil(t, current.Reference.Source)
	require.Empty(t, current.Reference.Authorities)
	require.NotNil(t, current.Reference.PaymentRetirement)
	require.Equal(t, "edit", current.Phase)
	require.Equal(t, opened.ID, current.Target)
	require.EqualValues(t, 1, current.Attempt)
	var message int64
	var visible bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT message_id,visible FROM bot.order_cards
 WHERE owner='alice' AND card_key=$1`, "payment:"+order.ID).Scan(&message, &visible))
	require.Equal(t, opened.ID, message)
	require.Equal(t, refusal, visible)
	if refusal {
		require.Equal(t, delivery.Rejected, current.State)
		require.Zero(t, current.MessageID)
		require.False(t, current.ContinuationDone)
	} else {
		require.Equal(t, delivery.Succeeded, current.State)
		require.Equal(t, opened.ID, current.MessageID)
		require.True(t, current.ContinuationDone)
	}
	assertPaymentRetirementQueue(t, f, current, string(current.State))
}

func TestPaymentUnavailableContextRestorationKeepsProjection(t *testing.T) {
	t.Parallel()
	for _, opening := range []string{"derived", "manual_model", "model_manual"} {
		for _, afterSend := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after_send_%t", opening, afterSend), func(t *testing.T) {
				t.Parallel()
				f, order, opened, _ := openedUnavailablePaymentSource(t, opening)
				_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
				require.NoError(t, err)
				require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
				intent := queuedPaymentRetirement(t, f)
				assertRestoredPaymentProjection(t, f, order, opened, intent, afterSend)
			})
		}
	}

	runPaymentOpeningReceiptLossCases(t)
}

func assertRestoredPaymentProjection(
	t *testing.T, f *fixture, order orders.Order, opened telegram.Message, intent botdelivery.Intent, afterSend bool,
) {
	t.Helper()
	restore := func() error {
		_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='alice'`)
		return err
	}
	if afterSend {
		f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
			if r.URL.Path == "/internal/bot-delivery/receipt" {
				return restore()
			}
			return nil
		}}}
	} else {
		require.NoError(t, restore())
	}
	var wire atomic.Int64
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		wire.Add(1)
		return http.DefaultTransport.RoundTrip(r)
	})}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	current, err := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
	require.NoError(t, err)
	if afterSend {
		require.EqualValues(t, 1, wire.Load())
		require.Equal(t, delivery.Succeeded, current.State)
		require.Equal(t, opened.ID, current.MessageID)
		require.True(t, current.ContinuationDone)
	} else {
		require.Zero(t, wire.Load())
		require.Equal(t, delivery.Cancelled, current.State)
		require.Zero(t, current.Attempt)
	}
	var message int64
	var visible bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT message_id,visible FROM bot.order_cards
 WHERE owner='alice' AND card_key=$1`, "payment:"+order.ID).Scan(&message, &visible))
	require.Equal(t, opened.ID, message)
	require.True(t, visible)
}

func runPaymentRetirementHistoryCases(t *testing.T) {
	t.Helper()
	for _, route := range []string{"derived", "manual_model"} {
		for _, language := range []string{"en", "ru"} {
			for _, retry := range []bool{false, true} {
				t.Run(fmt.Sprintf("history_after_enqueue/%s/%s/retry_%t", route, language, retry), func(t *testing.T) {
					t.Parallel()
					testPaymentRetirementHistory(t, route, language, retry)
				})
			}
		}
	}
}

func testPaymentRetirementHistory(t *testing.T, route, language string, retry bool) {
	t.Helper()
	f, order, opened, source := openedUnavailablePaymentSource(t, route)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	intent := queuedPaymentRetirement(t, f)
	require.Nil(t, intent.Reference.Generation, "fixed cleanup must have no history fence")
	require.True(t, intent.Reference.CanonicalPaymentRetirement())
	if retry {
		f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{},
				Request:    r,
				Body: io.NopCloser(
					strings.NewReader(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`),
				),
			}, nil
		})}
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
		deferred, readErr := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
		require.NoError(t, readErr)
		require.Equal(t, delivery.Deferred, deferred.State)
		require.EqualValues(t, 1, deferred.Attempt)
		require.Equal(t, opened, paymentMessage(t, f))
		f.b.TG.HTTP = nil
	}
	orderDeliveryHistoryDelete(t, f, "alice")
	_, err = f.b.API.SetLanguage(t.Context(), "alice", language, false)
	require.NoError(t, err)
	if retry {
		waitExportBoundaryCandidate(t, f, intent.QueueReference(), 2*time.Second)
	}
	var edits, sends atomic.Int64
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits.Add(1)
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
	require.EqualValues(t, 1, edits.Load())
	require.Zero(t, sends.Load())
	current, err := botdelivery.Read(t.Context(), f.db, intent.BotID, intent.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, current.State)
	require.Equal(t, opened.ID, current.MessageID)
	require.True(t, current.ContinuationDone)
	wantAttempts := int64(1)
	if retry {
		wantAttempts = 2
	}
	require.Equal(t, wantAttempts, current.Attempt)
	want, err := i18n.Translate(
		language,
		i18n.PaymentUnavailable,
		map[string]string{"code": "payment_context_unavailable"},
	)
	require.NoError(t, err)
	for _, item := range chatMessages(t, f, 101) {
		if item.ID == opened.ID {
			require.Equal(t, want, item.Text)
			require.Empty(t, item.Markup.Rows)
		}
	}
	require.JSONEq(t, string(source), string(displayedPaymentSource(t, f, order.ID)))
	assertPaymentRetirementQueue(t, f, current, string(delivery.Succeeded))
}

func runPaymentOpeningReceiptLossCases(t *testing.T) {
	t.Helper()
	for _, language := range []string{"en", "ru"} {
		t.Run("opening_receipt_rights_loss/"+language, func(t *testing.T) {
			t.Parallel()
			testPaymentOpeningReceiptLoss(t, language)
		})
	}
}

func testPaymentOpeningReceiptLoss(t *testing.T, language string) {
	t.Helper()
	f, order, opened, _ := openedUnavailablePaymentSource(t, "derived")
	_, err := f.db.Exec(t.Context(), `UPDATE core.order_events
 SET transfer_instructions_localized='{"en":"observed opening private canary","ru":"observed opening private canary"}' WHERE id=$1`, order.EventID)
	require.NoError(t, err)
	handle(t, f.b, orderClick(t, f, 101, 30005, "Payment methods"))
	pending := queuedPaymentOpening(t, f)
	var changed atomic.Bool
	f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
		if r.URL.Path == "/internal/bot-delivery/receipt" && !changed.Swap(true) {
			_, updateErr := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
			return updateErr
		}
		return nil
	}}}
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), pending.QueueReference()))
	require.True(t, changed.Load())
	sent, err := botdelivery.Read(t.Context(), f.db, pending.BotID, pending.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, sent.State)
	require.Equal(t, opened.ID, sent.MessageID)
	require.True(t, sent.ContinuationDone)
	require.JSONEq(t, `{"original":true,"source":null}`, string(displayedPaymentSource(t, f, order.ID)))
	retirement := queuedPaymentRetirement(t, f)
	require.Equal(t, pending.Operation, retirement.Reference.PaymentRetirement.Operation)
	require.Equal(t, pending.Effect, retirement.Reference.PaymentRetirement.Effect)
	_, err = f.b.API.SetLanguage(t.Context(), "alice", language, false)
	require.NoError(t, err)
	assertUnavailablePaymentDelivery(t, f, order, opened, retirement, language, false)
	for _, item := range chatMessages(t, f, 101) {
		require.NotContains(t, item.Text, "observed opening private canary")
	}
}
