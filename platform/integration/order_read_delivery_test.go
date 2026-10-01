package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestModelPaymentInstructionsUseCanonicalReadAction(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx := t.Context()
			_, err := f.b.API.SetLanguage(ctx, "alice", language, false)
			require.NoError(t, err)
			order, err := f.b.API.ExecuteOrder(
				ctx,
				"alice",
				orders.Command{
					EventID: "sandbox-festival",
					Name:    "create",
					Origin:  "manual",
					Key:     "instruction-order",
					Choice:  orderChoice("preparty"),
				},
			)
			require.NoError(t, err)
			f.model.plan = agent.Plan{
				View:        agent.OrdersView,
				OrderAction: &agent.OrderProposal{Name: orders.ActionPaymentInstructions, OrderID: order.ID},
			}
			handleVisible(t, f.b, message(29801, 101, "Show payment instructions for order "+order.ID))
			require.Contains(t, paymentMessage(t, f).Text, order.ID)
			pref, prefErr := f.b.API.Preferences(ctx, "alice")
			require.NoError(t, prefErr)
			require.Equal(t, language, pref.Language)
			prefix := "Payment for order"
			if language == "ru" {
				prefix = "Оплата заказа"
			}
			require.True(t, strings.HasPrefix(paymentMessage(t, f).Text, prefix))
			saved, err := (interaction.Store{DB: f.db}).Load(ctx, "alice", 29801)
			require.NoError(t, err)
			require.NotNil(t, saved.OrderCommand)
			require.Equal(t, orders.ActionPaymentInstructions, saved.OrderCommand.Name)
			current, err := f.b.API.Order(ctx, "alice", order.EventID, order.ID)
			require.NoError(t, err)
			require.Equal(t, order.Version, current.Version)
			var refused bool
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner='alice' AND update_id=29801 AND kind='order_error')`).
					Scan(&refused),
			)
			require.False(t, refused)
		})
	}
}

func orderDeliveryHistoryDelete(t *testing.T, f *fixture, owner string) {
	t.Helper()
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner=$1 AND origin='original' AND omission_reason<>'deleted' ORDER BY id LIMIT 1`, owner).
			Scan(&id),
	)
	require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(t.Context(), owner, id))
}

func TestOrderExportRechecksSourceAndCurrentGrantBeforeSend(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"model_source", "model_grant", "manual_grant", "script_grant"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			testOrderExportSnapshotBoundary(t, mode)
		})
	}
}

func testOrderExportSnapshotBoundary(t *testing.T, mode string) {
	t.Helper()
	f := setup(t)
	seedSurvivingOrderExportGrant(t, f, mode)
	fired := false
	f.b.API.HTTP = &http.Client{
		Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
			response, err := http.DefaultTransport.RoundTrip(r)
			if err == nil && response.StatusCode == http.StatusOK && strings.HasSuffix(r.URL.Path, "/export") &&
				!fired {
				body, bodyErr := io.ReadAll(response.Body)
				require.NoError(t, bodyErr)
				require.NoError(t, response.Body.Close())
				require.NotEmpty(t, body)
				response.Body = io.NopCloser(bytes.NewReader(body))
				fired = true
				if mode == "model_source" {
					orderDeliveryHistoryDelete(t, f, "bob")
				} else {
					_, writeErr := f.db.Exec(
						t.Context(),
						`DELETE FROM core.order_admins WHERE owner='bob' AND event_id='sandbox-festival'`,
					)
					require.NoError(t, writeErr)
				}
			}
			return response, err
		}),
	}
	f.b.Host.HTTP = f.b.API.HTTP
	f.model.plan = agent.Plan{View: agent.OrdersView, OrderAction: &agent.OrderProposal{Name: "export"}}
	update := message(29802, 202, "Export orders")
	if mode == "manual_grant" {
		update = modernExportCallback(t, f)
	}
	if mode == "script_grant" {
		f.b.Scripts = scopeVM{}
		f.b.Model = &knowledgeModel{
			plans: []agent.Plan{
				{
					View: agent.OrdersView,
					ScriptAction: &agent.ScriptProposal{
						Code:      `try { return tools.orders.export({}); } catch(e) { return {denied:true}; }`,
						InputJSON: "null",
					},
				},
				{View: agent.OrdersView, Text: "Checked"},
			},
		}
	}
	wire := orderSnapshotWireCounter(f)
	require.NoError(t, f.b.Handle(t.Context(), update))
	require.False(t, fired, "admission does not render an export")
	family := "order_export"
	if mode == "script_grant" {
		family = "modern_order_export"
	}
	operation, effect := botdelivery.ResultOperation("bob", update.ID, "document:"+family+":sandbox-festival:")
	ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	before, readErr := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, readErr)
	require.Equal(t, delivery.Deferred, before.State)
	if mode != "manual_grant" {
		require.NotNil(t, before.Reference.Source)
	}
	waitExportBoundaryCandidate(t, f, ref, time.Second)
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), ref))
	require.True(t, fired, "test reached the completed export snapshot")
	assertOrderSnapshotCancelled(t, f, before, wire)
	assertSurvivingOrderExportGrant(t, f, mode)
	require.Empty(t, exportDocuments(t, f, 202), "late source/grant revocation must prevent Telegram exposure")
}

func TestPaymentCardKeepsOriginalSourceThroughRefreshAndFallback(t *testing.T) {
	t.Parallel()
	for _, fallback := range []bool{false, true} {
		t.Run(
			fmt.Sprintf("fallback_%t", fallback),
			func(t *testing.T) { t.Parallel(); testPaymentCardSource(t, fallback) },
		)
	}
}
func testPaymentCardSource(t *testing.T, fallback bool) {
	t.Helper()
	f := setup(t)
	ctx := t.Context()
	_, err := f.b.API.SetLanguage(ctx, "alice", "en", false)
	require.NoError(t, err)
	order, err := f.b.API.ExecuteOrder(
		ctx,
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "payment-source",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	f.model.plan = agent.Plan{
		View:        agent.OrdersView,
		OrderAction: &agent.OrderProposal{Name: orders.ActionPaymentInstructions, OrderID: order.ID},
	}
	handleVisible(t, f.b, message(29803, 101, "Show payment instructions for "+order.ID))
	before := paymentMessage(t, f)
	var originalSource []byte
	require.NoError(t, f.db.QueryRow(
		ctx,
		`SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=0 AND kind=$1`,
		"payment_source:"+order.ID,
	).
		Scan(&originalSource))
	var bound bool
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT content->'source'<>'null'::jsonb FROM bot.interactions WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID).
			Scan(&bound),
	)
	require.True(t, bound)
	_, err = f.db.Exec(
		ctx,
		`UPDATE core.order_events SET transfer_instructions_localized='{"en":"new payment canary"}' WHERE id=$1`,
		order.EventID,
	)
	require.NoError(t, err)
	fallbackSeen := false
	var sends atomic.Int64
	if fallback {
		var historyID int64
		require.NoError(t, f.db.QueryRow(
			ctx,
			`SELECT id FROM core.conversation_events WHERE owner='alice' AND origin='original' AND omission_reason<>'deleted' ORDER BY id LIMIT 1`,
		).
			Scan(&historyID))
		f.b.TG.HTTP = &http.Client{Transport: paymentSourceEditRefusal(f, before.ID, historyID, &fallbackSeen, &sends)}
		require.NoError(t, f.b.RenderOrders(ctx, "alice", 101))
		var operation, effect string
		require.NoError(t, f.db.QueryRow(ctx, `SELECT operation_key,effect_key FROM bot.delivery_intents
			WHERE owner='alice' AND reference->>'family'='payment' AND state='pending'
			ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
		ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
		pumpBotDeliveries(t, f.b)
		require.True(t, fallbackSeen, "provider must reject the exact payment edit after source revocation")
		require.NoError(t, f.b.DeliverBotIntent(ctx, ref))
		cancelled, readErr := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, ref, false)
		require.NoError(t, readErr)
		require.Equal(t, delivery.Cancelled, cancelled.State)
		require.Zero(t, sends.Load(), "revoked payment source must prevent fallback sends")
	} else {
		orderDeliveryHistoryDelete(t, f, "alice")
		require.Error(t, f.b.RenderOrders(ctx, "alice", 101))
	}
	require.Equal(t, fallback, fallbackSeen)
	var retainedSource []byte
	require.NoError(t, f.db.QueryRow(
		ctx,
		`SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=0 AND kind=$1`,
		"payment_source:"+order.ID,
	).
		Scan(&retainedSource))
	require.JSONEq(t, string(originalSource), string(retainedSource))
	for _, item := range chatMessages(t, f, 101) {
		require.NotContains(t, item.Text, "new payment canary")
		if item.ID == before.ID {
			require.Equal(t, before.Text, item.Text)
		}
	}
	f.b.TG.HTTP = nil
	handleVisible(t, f.b, orderClick(t, f, 101, 29804, "Payment methods"))
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT content->'source'<>'null'::jsonb FROM bot.interactions WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID).
			Scan(&bound),
	)
	require.False(t, bound, "explicit manual open establishes fresh independent content")
	found := false
	for _, item := range chatMessages(t, f, 101) {
		found = found || strings.Contains(item.Text, "new payment canary")
	}
	require.True(t, found)
}

func paymentSourceEditRefusal(
	f *fixture,
	target, historyID int64,
	seen *bool,
	sends *atomic.Int64,
) qaArchiveBoundaryTransport {
	return func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			sends.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			raw, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				return nil, readErr
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var payload telegram.Send
			if readErr = json.Unmarshal(raw, &payload); readErr != nil {
				return nil, readErr
			}
			if payload.MessageID != target {
				return http.DefaultTransport.RoundTrip(r)
			}
			*seen = true
			if readErr = (conversation.Service{DB: f.db}).DeleteContent(
				r.Context(),
				"alice",
				historyID,
			); readErr != nil {
				return nil, readErr
			}
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{},
				Body: io.NopCloser(
					strings.NewReader(`{"ok":false,"error_code":400,"description":"message to edit not found"}`),
				),
				Request: r,
			}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	}
}

func TestPaymentUnavailableContextRetiresRevokedSource(t *testing.T) {
	t.Parallel()
	for _, opening := range []string{"derived", "manual_model", "model_manual"} {
		for _, language := range []string{"en", "ru"} {
			for _, queued := range []bool{false, true} {
				for _, refusal := range []bool{false, true} {
					t.Run(
						fmt.Sprintf("%s/%s/queued_%t/refusal_%t", opening, language, queued, refusal),
						func(t *testing.T) {
							t.Parallel()
							testUnavailablePaymentSource(t, opening, language, queued, refusal)
						},
					)
				}
			}
		}
	}
}

func testUnavailablePaymentSource(t *testing.T, opening, language string, queued, refusal bool) {
	t.Helper()
	f, order, opened, originalSource := openedUnavailablePaymentSource(t, opening)
	var pending botdelivery.Intent
	if queued {
		_, err := f.db.Exec(t.Context(), `UPDATE core.order_events
 SET transfer_instructions_localized='{"en":"late private payment canary"}' WHERE id=$1`, order.EventID)
		require.NoError(t, err)
		require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
		pending = queuedLivePayment(t, f)
	}
	orderDeliveryHistoryDelete(t, f, "alice")
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	if queued {
		require.NoError(t, f.b.DeliverBotIntent(t.Context(), pending.QueueReference()))
		current, readErr := botdelivery.Read(t.Context(), f.db, pending.BotID, pending.QueueReference(), false)
		require.NoError(t, readErr)
		require.Equal(t, delivery.Cancelled, current.State)
		require.Zero(t, current.Attempt)
	} else {
		require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	}
	intent := queuedPaymentRetirement(t, f)
	_, err = f.b.API.SetLanguage(t.Context(), "alice", language, false)
	require.NoError(t, err)
	assertUnavailablePaymentDelivery(t, f, order, opened, intent, language, refusal)
	var retainedSource []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID).Scan(&retainedSource))
	require.JSONEq(t, string(originalSource), string(retainedSource))
	for _, item := range chatMessages(t, f, 101) {
		require.NotContains(t, item.Text, "late private payment canary")
	}
}

func openedUnavailablePaymentSource(t *testing.T, opening string) (*fixture, orders.Order, telegram.Message, []byte) {
	t.Helper()
	var f *fixture
	var order orders.Order
	var err error
	if opening == "manual_model" {
		f, order, _ = openedPaymentFixture(t)
	} else {
		f = setup(t)
		order, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
			EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "retire-bound",
			Choice: orderChoice("preparty"),
		})
		require.NoError(t, err)
		_, err = f.b.API.SetLanguage(t.Context(), "alice", "en", false)
		require.NoError(t, err)
	}
	f.model.plan = agent.Plan{
		View:        agent.OrdersView,
		OrderAction: &agent.OrderProposal{Name: orders.ActionPaymentInstructions, OrderID: order.ID},
	}
	handleVisible(t, f.b, message(29805, 101, "Show payment instructions for "+order.ID))
	opened := paymentMessage(t, f)
	var originalSource, receiptSource []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID).Scan(&originalSource))
	if opening == "manual_model" {
		require.Contains(t, string(originalSource), `"original": true`)
	} else {
		require.Contains(t, string(originalSource), `"original": false`)
	}
	if opening == "model_manual" {
		handleVisible(t, f.b, orderClick(t, f, 101, 29806, "Payment methods"))
		after := paymentMessage(t, f)
		require.Equal(t, opened.ID, after.ID)
		require.Equal(t, opened.Text, after.Text)
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT jsonb_build_object('original',reference->'source' IS NULL,'source',reference->'source')
 FROM bot.delivery_intents WHERE owner='alice' AND message_id=$1 AND state='sent' AND continuation_done
 AND reference->>'family'='payment' ORDER BY attempted_at DESC LIMIT 1`, opened.ID).
			Scan(&receiptSource),
	)
	require.JSONEq(
		t,
		string(originalSource),
		string(receiptSource),
		"the displayed receipt must carry its actual source",
	)
	var retained []byte
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=0 AND kind=$1`, "payment_source:"+order.ID).Scan(&retained))
	require.JSONEq(t, string(originalSource), string(retained), "a no-op must retain the successful source binding")
	var intents int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents
 WHERE owner='alice' AND reference->>'family'='payment'`).Scan(&intents))
	require.Equal(t, 1, intents, "same-body opening must not create another payment transport intent")
	return f, order, opened, originalSource
}

func queuedLivePayment(t *testing.T, f *fixture) botdelivery.Intent {
	t.Helper()
	var operation, effect string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner='alice' AND state='pending' AND reference->>'family'='payment'
 AND COALESCE(reference->>'notice','')='' ORDER BY created_at DESC LIMIT 1`).Scan(&operation, &effect))
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
	t.Fatal("live payment intent did not become the lane head")
	return botdelivery.Intent{}
}

func TestModernProofRechecksExactBindingAfterDownload(t *testing.T) {
	t.Parallel()
	for _, revoke := range []string{"version", "attempt", "proof_file", "history", "unchanged"} {
		t.Run(revoke, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx := t.Context()
			service := orders.Service{DB: f.db}
			require.NoError(
				t,
				(conversation.Service{DB: f.db}).AppendOriginal(
					ctx,
					"alice",
					"proof-source",
					"user",
					"Original private receipt request",
				),
			)
			order, err := service.Execute(
				ctx,
				"alice",
				orders.Command{
					EventID: "sandbox-festival",
					Name:    "create",
					Origin:  "manual",
					Key:     "late-proof",
					Choice:  orderChoice("preparty"),
				},
			)
			require.NoError(t, err)
			command := orderCommand("proof", order)
			command.ProofFile = uploadProof(t, service, "alice")
			order, err = service.Execute(ctx, "alice", command)
			require.NoError(t, err)
			expected, err := service.OrderProof(ctx, "alice", order.EventID, order.ID)
			require.NoError(t, err)
			wire := orderSnapshotWireCounter(f)
			fired := false
			downloads := 0
			f.b.API.HTTP = &http.Client{
				Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
					response, readErr := http.DefaultTransport.RoundTrip(r)
					if readErr == nil && response.StatusCode == http.StatusOK &&
						strings.HasSuffix(r.URL.Path, "/proof/file") {
						downloads++
						require.False(t, fired, "terminal replay must not download private bytes again")
						body, bodyErr := io.ReadAll(response.Body)
						require.NoError(t, bodyErr)
						require.NoError(t, response.Body.Close())
						require.Equal(t, expected.Body, body)
						require.Equal(t, expected.ID, response.Header.Get("X-Proof-Id"))
						require.Equal(
							t,
							strconv.FormatInt(expected.Version, 10),
							response.Header.Get("X-Order-Version"),
						)
						require.Equal(t, expected.Attempt, response.Header.Get("X-Payment-Attempt"))
						response.Body = io.NopCloser(bytes.NewReader(body))
						fired = true
						mutateOrderProofSnapshot(t, f, revoke, order, expected)
					}
					return response, readErr
				}),
			}
			f.b.Host.HTTP = f.b.API.HTTP
			f.b.Scripts = scopeVM{}
			model := &knowledgeModel{
				plans: []agent.Plan{
					{
						View: agent.OrdersView,
						ScriptAction: &agent.ScriptProposal{
							Code: fmt.Sprintf(
								`tools.orders.inspect({order_id:%q});try{return tools.orders.proof({order_id:%q});}catch(e){return {denied:true};}`,
								order.ID,
								order.ID,
							),
							InputJSON: "null",
						},
					},
					{View: agent.OrdersView, Text: "Checked"},
				},
			}
			f.b.Model = model
			require.NoError(t, f.b.Handle(ctx, message(29805, 101, "Show receipt for "+order.ID)))
			require.False(t, fired, "admission must not download the proof")
			operation, effect := botdelivery.ResultOperation("alice", 29805,
				"document:modern_order_proof:sandbox-festival:"+order.ID)
			ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
			before, readErr := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, ref, false)
			require.NoError(t, readErr)
			require.Equal(t, delivery.Deferred, before.State)
			require.Equal(t, expected.Version, before.Reference.Version)
			require.Equal(t, expected.Attempt, before.Reference.ProofAttempt)
			require.NotNil(t, before.Reference.Source)
			waitExportBoundaryCandidate(t, f, ref, time.Second)
			require.NoError(t, f.b.DeliverBotIntent(ctx, ref))
			require.NotEmpty(t, model.inputs)
			require.Contains(
				t,
				fmt.Sprint(model.inputs[0].History),
				"Original private receipt request",
				"the provider observed the source before the download barrier",
			)
			require.True(t, fired)
			require.Equal(t, 1, downloads)
			if revoke == "unchanged" {
				assertOrderSnapshotProof(t, f, before, expected, wire)
			} else {
				assertOrderSnapshotCancelled(t, f, before, wire)
				for _, item := range chatMessages(t, f, 101) {
					require.Nil(t, item.Document, "stale proof bytes must never reach Telegram")
				}
			}
			require.Equal(t, 1, downloads, "terminal replay must retain the completed snapshot identity")
		})
	}
}

func TestModernExportRetriesAuthorityOutageBeforeTransport(t *testing.T) {
	t.Parallel()
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprintf("revoked_%t", revoked), func(t *testing.T) {
			t.Parallel()
			testModernExportAuthorityRecovery(t, revoked)
		})
	}
}

type modernExportAuthorityProbe struct {
	t           *testing.T
	f           *fixture
	ref         delivery.Reference
	snapshots   int
	unavailable bool
	wire        int
	body        []byte
}

func (p *modernExportAuthorityProbe) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/business-capabilities") && p.snapshots == 1 && !p.unavailable {
		p.unavailable = true
		i, err := botdelivery.Read(p.t.Context(), p.f.db, p.f.b.Delivery.BotID, p.ref, false)
		require.NoError(p.t, err)
		require.Equal(p.t, delivery.Deferred, i.State)
		require.Zero(p.t, i.Attempt)
		require.Zero(p.t, i.MessageID)
		require.Zero(p.t, p.wire)
		require.Empty(p.t, exportDocuments(p.t, p.f, 202))
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"authority_unavailable"}`)),
			Request:    r,
		}, nil
	}
	if strings.HasSuffix(r.URL.Path, "/export") {
		p.snapshots++
	}
	response, err := http.DefaultTransport.RoundTrip(r)
	if err == nil && strings.HasSuffix(r.URL.Path, "/export") && response.StatusCode == http.StatusOK {
		p.body, err = io.ReadAll(response.Body)
		require.NoError(p.t, err)
		require.NoError(p.t, response.Body.Close())
		response.Body = io.NopCloser(bytes.NewReader(p.body))
	}
	return response, err
}

func modernExportProjectionCount(t *testing.T, f *fixture) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions
 WHERE owner='bob' AND update_id=29806 AND kind='modern_order_delivery:orders.export'`).Scan(&count))
	return count
}

func testModernExportAuthorityRecovery(t *testing.T, revoked bool) {
	t.Helper()
	f := setup(t)
	ctx := t.Context()
	operation, effect := botdelivery.ResultOperation("bob", 29806, "document:modern_order_export:sandbox-festival:")
	ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
	probe := &modernExportAuthorityProbe{t: t, f: f, ref: ref}
	f.b.API.HTTP = &http.Client{Transport: probe}
	f.b.Host.HTTP = f.b.API.HTTP
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendDocument") {
			probe.wire++
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	var operationsBefore int
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM core.order_operations`).Scan(&operationsBefore))
	result := runModernVM(
		t,
		f,
		29806,
		202,
		"Export orders",
		`const first=tools.orders.export({});const second=tools.orders.export({});return {first:first.status,second:second.status};`,
	)
	require.JSONEq(t, `{"first":"pending","second":"pending"}`, string(result))
	require.Zero(t, probe.snapshots)
	require.Zero(t, probe.wire)
	require.Zero(t, modernExportProjectionCount(t, f))
	original, err := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, original.State)
	err = f.b.DeliverBotIntent(ctx, ref)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusServiceUnavailable, problem.Status)
	require.Equal(t, "authority_unavailable", problem.Code)
	require.True(t, probe.unavailable)
	require.Equal(t, 1, probe.snapshots)
	require.Zero(t, probe.wire)
	require.Empty(t, exportDocuments(t, f, 202))
	require.Zero(t, modernExportProjectionCount(t, f))
	pending, err := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.Equal(t, delivery.Deferred, pending.State)
	require.Equal(t, original.Reference, pending.Reference)
	require.Zero(t, pending.Attempt)
	require.Zero(t, pending.MessageID)
	require.False(t, pending.ContinuationDone)
	require.True(t, pending.NotBefore.After(original.NotBefore))
	var queueState string
	var queueDeadline time.Time
	require.NoError(t, f.db.QueryRow(ctx, `SELECT state,not_before FROM core.delivery_queue
 WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key=$4`,
		f.b.Delivery.BotID, string(ref.Owner), ref.Key, ref.Effect).Scan(&queueState, &queueDeadline))
	require.Equal(t, string(delivery.Deferred), queueState)
	require.True(t, queueDeadline.Equal(pending.NotBefore))
	if revoked {
		_, err = f.db.Exec(ctx, `DELETE FROM core.order_admins WHERE owner='bob' AND event_id='sandbox-festival'`)
		require.NoError(t, err)
	}
	// Preserve the real configured fallback. Selection, not a test SQL rewrite,
	// determines when the same durable effect can be retried.
	require.Eventually(t, func() bool {
		for _, entry := range botDeliveryCandidates(t, f.b) {
			if entry.Reference == ref {
				return true
			}
		}
		return false
	}, f.b.Delivery.Fallback+5*time.Second, 50*time.Millisecond)
	require.NoError(t, f.b.DeliverBotIntent(ctx, ref))
	after, err := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.Equal(t, original.Reference, after.Reference)
	require.Equal(t, 2, probe.snapshots)
	if revoked {
		require.Equal(t, delivery.Cancelled, after.State)
		require.Zero(t, after.MessageID)
		require.Zero(t, probe.wire)
		require.Zero(t, modernExportProjectionCount(t, f))
		require.Empty(t, exportDocuments(t, f, 202))
	} else {
		require.Equal(t, delivery.Succeeded, after.State)
		require.Positive(t, after.MessageID)
		require.True(t, after.ContinuationDone)
		require.Equal(t, 1, modernExportProjectionCount(t, f))
		require.Equal(t, 1, probe.wire)
		documents := exportDocuments(t, f, 202)
		require.Len(t, documents, 1)
		body, readErr := f.b.TG.Download(ctx, telegram.Document{FileID: documents[0], Filename: "orders.xlsx"})
		require.NoError(t, readErr)
		require.NotEmpty(t, probe.body)
		require.Equal(t, probe.body, body)
	}
	require.NoError(t, f.b.DeliverBotIntent(ctx, ref))
	require.Equal(t, 2, probe.snapshots)
	final, err := botdelivery.Read(ctx, f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	require.Equal(t, after, final)
	if !revoked {
		handle(t, f.b, message(29806, 202, "Export orders"))
		require.Equal(t, 1, probe.wire)
		require.Equal(t, 2, probe.snapshots)
		require.Len(t, exportDocuments(t, f, 202), 1)
	}
	var operationsAfter int
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM core.order_operations`).Scan(&operationsAfter))
	require.Equal(t, operationsBefore, operationsAfter)
}

func orderSnapshotWireCounter(f *fixture) *atomic.Int64 {
	var wire atomic.Int64
	f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendDocument") {
			wire.Add(1)
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	return &wire
}

func assertOrderSnapshotCancelled(t *testing.T, f *fixture, before botdelivery.Intent, wire *atomic.Int64) {
	t.Helper()
	after, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, before.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, before.Reference, after.Reference)
	require.Equal(t, delivery.Cancelled, after.State)
	require.Zero(t, after.Attempt)
	require.Zero(t, after.MessageID)
	require.Nil(t, after.Receipt.Document)
	require.Zero(t, wire.Load())
	var projections int
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT count(*) FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3",
		before.Owner, before.Reference.Update, before.Reference.Continuation.Key).Scan(&projections))
	require.Zero(t, projections, "a pre-send cancellation cannot fabricate a document receipt")
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), before.QueueReference()))
	final, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, before.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, after, final)
	require.Zero(t, wire.Load())
}

func assertOrderSnapshotProof(
	t *testing.T,
	f *fixture,
	before botdelivery.Intent,
	expected orders.Proof,
	wire *atomic.Int64,
) {
	t.Helper()
	after, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, before.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, before.Reference, after.Reference)
	require.Equal(t, delivery.Succeeded, after.State)
	require.Positive(t, after.MessageID)
	require.True(t, after.ContinuationDone)
	require.NotNil(t, after.Receipt.Document)
	digest := sha256.Sum256(expected.Body)
	require.Equal(t, &botdelivery.DocumentReceipt{
		Filename: expected.Filename, SHA256: hex.EncodeToString(digest[:]), Bytes: len(expected.Body),
	}, after.Receipt.Document)
	var projection botdelivery.ModernReceipt
	require.NoError(t, f.db.QueryRow(t.Context(),
		"SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3",
		before.Owner, before.Reference.Update, before.Reference.Continuation.Key).Scan(&projection))
	require.Equal(t, botdelivery.ModernReceipt{
		Status: "delivered", EventID: before.Reference.Event, ChatID: before.Chat,
		Filename: expected.Filename, SHA256: hex.EncodeToString(digest[:]),
		Bytes: len(expected.Body), MessageID: after.MessageID,
	}, projection)
	documents := 0
	for _, item := range chatMessages(t, f, before.Chat) {
		if item.Document == nil {
			continue
		}
		documents++
		require.Equal(t, after.MessageID, item.ID)
		require.Equal(t, expected.Filename, item.Document.Filename)
		body, readErr := f.b.TG.Download(t.Context(), *item.Document)
		require.NoError(t, readErr)
		require.Equal(t, expected.Body, body)
	}
	require.Equal(t, 1, documents)
	require.EqualValues(t, 1, wire.Load())
	require.NoError(t, f.b.DeliverBotIntent(t.Context(), before.QueueReference()))
	final, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, before.QueueReference(), false)
	require.NoError(t, err)
	require.Equal(t, after, final)
	require.EqualValues(t, 1, wire.Load())
}

func mutateOrderProofSnapshot(
	t *testing.T,
	f *fixture,
	mode string,
	order orders.Order,
	original orders.Proof,
) {
	t.Helper()
	service := orders.Service{DB: f.db}
	expected := original
	var query string
	var value any
	switch mode {
	case "history":
		orderDeliveryHistoryDelete(t, f, "alice")
		return
	case "unchanged":
		return
	case "version":
		query, value = "UPDATE core.orders SET version=$2 WHERE id=$1", original.Version+1
		expected.Version++
	case "attempt":
		query, value = "UPDATE core.orders SET attempt=$2 WHERE id=$1", original.Attempt+"-replacement"
		expected.Attempt = original.Attempt + "-replacement"
	case "proof_file":
		body := append(bytes.Clone(original.Body), []byte("replacement proof")...)
		replacement, err := service.UploadProof(t.Context(), "alice", original.Filename, body)
		require.NoError(t, err)
		require.NotEqual(t, original.ID, replacement.ID)
		query, value = "UPDATE core.orders SET proof_file=$2 WHERE id=$1", replacement.ID
		expected.ID, expected.Body = replacement.ID, body
	default:
		t.Fatalf("unsupported proof mutation %q", mode)
	}
	tag, err := f.db.Exec(t.Context(), query, order.ID, value)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	current, err := service.OrderProof(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, expected, current, "only the selected current proof binding may change")
}

const survivingOrderExportEvent = "other-export-event"

func seedSurvivingOrderExportGrant(t *testing.T, f *fixture, mode string) {
	t.Helper()
	if mode == "model_source" {
		return
	}
	_, err := f.db.Exec(
		t.Context(),
		"INSERT INTO core.order_events(id,deadline,menu,extras) SELECT $1,deadline,menu,extras FROM core.order_events WHERE id='sandbox-festival'",
		survivingOrderExportEvent,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(),
		"INSERT INTO core.order_admins(event_id,owner,country) VALUES($1,'bob','be')",
		survivingOrderExportEvent)
	require.NoError(t, err)
}

func assertSurvivingOrderExportGrant(t *testing.T, f *fixture, mode string) {
	t.Helper()
	if mode == "model_source" {
		return
	}
	capability, err := f.b.API.BusinessCapabilities(t.Context(), "bob", survivingOrderExportEvent)
	require.NoError(t, err)
	require.True(t, capability.CanBook)
	require.True(t, capability.CanExportOrders, "the unrelated export grant must survive")
	body, err := f.b.API.ExportOrders(t.Context(), "bob", survivingOrderExportEvent)
	require.NoError(t, err)
	require.NotEmpty(t, body, "the surviving grant must authorize a real export")
	_, err = f.b.API.ExportOrders(t.Context(), "bob", "sandbox-festival")
	requireCode(t, err, "forbidden")
}
