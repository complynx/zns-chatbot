package integration_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func queueRefundCard(t *testing.T, f *fixture, owner string, chat int64, task orders.RefundTask) botdelivery.Intent {
	t.Helper()
	current, err := f.b.API.Refund(t.Context(), owner, task.ID)
	require.NoError(t, err)
	binding := current.DeliveryRead()
	generation, err := f.b.API.HistoryGeneration(t.Context(), owner)
	require.NoError(t, err)
	id := strconv.FormatInt(task.ID, 10)
	ref := botdelivery.Reference{
		Kind: botdelivery.CardIntent, Family: "order_refund", Event: task.EventID,
		Object: id, CardKey: "refund:" + id, Refund: &binding, Generation: &generation,
		Continuation: botdelivery.Continuation{Kind: "order_card", Key: "refund:" + id},
	}
	require.NoError(t, f.b.Host.EnqueueBotCard(t.Context(), botdelivery.CardRequest{
		Owner: owner, Chat: chat, Reference: ref,
	}))
	return refundCardIntent(t, f, owner, ref.Family)
}

func refundCardIntent(t *testing.T, f *fixture, owner, family string) botdelivery.Intent {
	t.Helper()
	var ref delivery.Reference
	ref.Owner = delivery.Bot
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE owner=$1 AND reference->>'family'=$2 ORDER BY created_at DESC LIMIT 1`, owner, family).Scan(&ref.Key, &ref.Effect))
	intent, err := botdelivery.Read(t.Context(), f.db, f.b.Delivery.BotID, ref, false)
	require.NoError(t, err)
	return intent
}

func TestRefundCardRechecksAuthorityAfterRender(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"unchanged", "reassign", "revoke", "version"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			task := refundUIFixture(t, f)
			assignRefundAmbassador(t, f)
			intent := queueRefundCard(t, f, "bob", 202, task)
			var intercepted atomic.Bool
			f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
				if r.URL.Path != "/internal/bot-delivery/begin" || intercepted.Swap(true) {
					return nil
				}
				var err error
				switch change {
				case "reassign":
					_, err = f.db.Exec(
						r.Context(),
						`UPDATE core.pass_bookings SET payment_admin='alice' WHERE owner='alice'`,
					)
				case "revoke":
					_, err = f.db.Exec(r.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
				case "version":
					_, err = f.db.Exec(
						r.Context(),
						`UPDATE core.order_refund_tasks SET version=version+1 WHERE id=$1`,
						task.ID,
					)
				}
				return err
			}}}
			wire := &boundaryTransport{}
			f.b.TG.HTTP = &http.Client{Transport: wire}
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
			require.True(t, intercepted.Load(), "mutation must occur after the private card was rendered")
			current := refundCardIntent(t, f, "bob", "order_refund")
			if change == "unchanged" {
				require.Equal(t, delivery.Succeeded, current.State)
				require.EqualValues(t, 1, wire.calls.Load())
				return
			}
			require.Equal(t, delivery.Cancelled, current.State)
			require.Zero(t, current.Attempt)
			require.Zero(t, wire.calls.Load())
			assignRefundAmbassador(t, f)
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
			require.Zero(t, wire.calls.Load(), "restoring authority cannot revive the cancelled disclosure")
		})
	}
}

func TestRefundCardBindsUnassignedReaderProjection(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"alice", "bob"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			task := refundUIFixture(t, f)
			chat := int64(101)
			if owner == "bob" {
				chat = 202
				_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
				require.NoError(t, err)
			}
			intent := queueRefundCard(t, f, owner, chat, task)
			var intercepted atomic.Bool
			f.b.Host.HTTP = &http.Client{Transport: &boundaryTransport{before: func(r *http.Request) error {
				if r.URL.Path != "/internal/bot-delivery/begin" || intercepted.Swap(true) {
					return nil
				}
				if owner == "alice" {
					assignRefundAmbassador(t, f)
					return nil
				}
				_, err := f.db.Exec(r.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
				return err
			}}}
			wire := &boundaryTransport{}
			f.b.TG.HTTP = &http.Client{Transport: wire}
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), intent.QueueReference()))
			require.True(t, intercepted.Load())
			require.Zero(t, wire.calls.Load())
			require.Equal(t, delivery.Cancelled, refundCardIntent(t, f, owner, "order_refund").State)
		})
	}
}

func TestRefundCardRedactionIsFixedKnownTargetEditOnly(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		t.Run(strconv.FormatBool(missing), func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			task := refundUIFixture(t, f)
			assignRefundAmbassador(t, f)
			original := queueRefundCard(t, f, "bob", 202, task)
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), original.QueueReference()))
			sent := refundCardIntent(t, f, "bob", "order_refund")
			require.Positive(t, sent.MessageID)
			_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
			require.NoError(t, err)
			ref := sent.Reference
			ref.Family, ref.Refund = "order_refund_redaction", nil
			ref.Continuation.Retired = true
			request := botdelivery.CardRequest{Owner: "bob", Chat: 202, Reference: ref}
			require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "a tombstone cannot create a message")
			request.Target = sent.MessageID + 10000
			require.Error(t, f.b.Host.EnqueueBotCard(t.Context(), request), "an unrelated target is not authorized")
			request.Target = sent.MessageID
			require.NoError(t, f.b.Host.EnqueueBotCard(t.Context(), request))
			redaction := refundCardIntent(t, f, "bob", ref.Family)
			var calls atomic.Int64
			if missing {
				f.b.TG.HTTP = &http.Client{
					Transport: qaArchiveBoundaryTransport(func(r *http.Request) (*http.Response, error) {
						calls.Add(1)
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
					}),
				}
			}
			require.NoError(t, f.b.DeliverBotIntent(t.Context(), redaction.QueueReference()))
			if missing {
				require.NoError(t, f.b.DeliverBotIntent(t.Context(), redaction.QueueReference()))
				require.EqualValues(t, 1, calls.Load(), "a missing target must never fall back to a new message")
				require.Equal(t, delivery.Cancelled, refundCardIntent(t, f, "bob", ref.Family).State)
				return
			}
			prefs, err := f.b.API.Preferences(t.Context(), "bob")
			require.NoError(t, err)
			tombstone, err := i18n.Translate(prefs.Language, i18n.OrderRetired, nil)
			require.NoError(t, err)
			messages := chatMessages(t, f, 202)
			require.Len(t, messages, 1)
			require.Equal(t, tombstone, messages[0].Text)
			require.Empty(t, messages[0].Markup.Rows)
		})
	}
}
