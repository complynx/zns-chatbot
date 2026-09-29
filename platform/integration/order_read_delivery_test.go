package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
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
			handle(t, f.b, message(29801, 101, "Show payment instructions for order "+order.ID))
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
			f := setup(t)
			fired := false
			f.b.API.HTTP = &http.Client{
				Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
					response, err := http.DefaultTransport.RoundTrip(r)
					if err == nil && strings.HasSuffix(r.URL.Path, "/export") && !fired {
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
			_ = f.b.Handle(t.Context(), update)
			require.True(t, fired, "test reached the completed export snapshot")
			require.Empty(t, exportDocuments(t, f, 202), "late source/grant revocation must prevent Telegram exposure")
		})
	}
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
	handle(t, f.b, message(29803, 101, "Show payment instructions for "+order.ID))
	before := paymentMessage(t, f)
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
	if fallback {
		f.b.TG.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/editMessageText") {
				fallbackSeen = true
				orderDeliveryHistoryDelete(t, f, "alice")
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
		})}
	} else {
		orderDeliveryHistoryDelete(t, f, "alice")
	}
	require.Error(t, f.b.RenderOrders(ctx, "alice", 101))
	require.Equal(t, fallback, fallbackSeen)
	for _, item := range chatMessages(t, f, 101) {
		require.NotContains(t, item.Text, "new payment canary")
		if item.ID == before.ID {
			require.Equal(t, before.Text, item.Text)
		}
	}
	f.b.TG.HTTP = nil
	handle(t, f.b, orderClick(t, f, 101, 29804, "Payment methods"))
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

func TestModernProofRechecksExactBindingAfterDownload(t *testing.T) {
	t.Parallel()
	for _, revoke := range []string{"version", "history"} {
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
			fired := false
			f.b.API.HTTP = &http.Client{
				Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
					response, readErr := http.DefaultTransport.RoundTrip(r)
					if readErr == nil && strings.HasSuffix(r.URL.Path, "/proof/file") && !fired {
						fired = true
						if revoke == "history" {
							orderDeliveryHistoryDelete(t, f, "alice")
						} else {
							_, writeErr := f.db.Exec(
								ctx,
								`UPDATE core.orders SET version=version+1 WHERE id=$1`,
								order.ID,
							)
							require.NoError(t, writeErr)
						}
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
			_ = f.b.Handle(ctx, message(29805, 101, "Show receipt for "+order.ID))
			require.NotEmpty(t, model.inputs)
			require.Contains(
				t,
				fmt.Sprint(model.inputs[0].History),
				"Original private receipt request",
				"the provider observed the source before the download barrier",
			)
			require.True(t, fired)
			for _, item := range chatMessages(t, f, 101) {
				require.Nil(t, item.Document, "stale proof bytes must never reach Telegram")
			}
			var status string
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT content->>'status' FROM bot.interactions WHERE owner='alice' AND update_id=29805 AND kind=$1`, "modern_order_delivery:orders.proof:"+order.ID).
					Scan(&status),
			)
			require.Equal(t, "blocked", status)
		})
	}
}

func TestModernExportRetriesAuthorityOutageBeforeTransport(t *testing.T) {
	t.Parallel()
	f := setup(t)
	snapshots := 0
	unavailable := false
	f.b.API.HTTP = &http.Client{Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/business-capabilities") && snapshots == 1 && !unavailable {
			unavailable = true
			var admitted int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=29806 AND kind='modern_order_delivery:orders.export'`).
					Scan(&admitted),
			)
			require.Equal(t, 1, admitted, "the fault occurs after durable admission but before Telegram")
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":"authority_unavailable"}`)),
				Request:    r,
			}, nil
		}
		if strings.HasSuffix(r.URL.Path, "/export") {
			snapshots++
			if snapshots == 2 {
				var admitted int
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=29806 AND kind='modern_order_delivery:orders.export'`).
						Scan(&admitted),
				)
				require.Zero(t, admitted, "known-not-attempted admission must not become a permanent uncertain receipt")
			}
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	f.b.Host.HTTP = f.b.API.HTTP
	result := runModernVM(
		t,
		f,
		29806,
		202,
		"Export orders",
		`let failed=false;try{tools.orders.export({});}catch(e){failed=true;}const retried=tools.orders.export({});return {failed,status:retried.status};`,
	)
	require.True(t, unavailable)
	require.Equal(t, 2, snapshots)
	require.JSONEq(t, `{"failed":true,"status":"delivered"}`, string(result))
	require.Len(t, exportDocuments(t, f, 202), 1)
}
