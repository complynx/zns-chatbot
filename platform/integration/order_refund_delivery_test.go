package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func refundRequestOutcome(t *testing.T, f refundFixture, state delivery.Kind) orders.Notification {
	t.Helper()
	count, err := f.service.RouteRefunds(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var id int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT notification_id FROM core.order_refund_tasks`).Scan(&id))
	notice, ready, err := f.service.PrepareNotification(t.Context(), id)
	require.NoError(t, err)
	require.True(t, ready)
	gate, err := f.service.BeginNotification(t.Context(), delivery.Attempt{ID: id, Generation: notice.DeliveryAttempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	if state == delivery.Sending {
		return notice
	}
	completion := orders.NotificationCompletion{
		ID:      id,
		Attempt: notice.DeliveryAttempt,
		Outcome: delivery.Outcome{Kind: state},
	}
	if state == delivery.Succeeded {
		completion.Text = "Refund request"
		completion.Outcome.MessageID = 101
	} else {
		completion.Outcome.Reason = "synthetic_failure"
	}
	require.NoError(t, f.service.CompleteNotification(t.Context(), completion))
	return notice
}

func routeRefundDue(t *testing.T, f refundFixture, want int) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `UPDATE core.order_refund_tasks SET next_route_at=now()`)
	require.NoError(t, err)
	count, err := f.service.RouteRefunds(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, count)
}

func TestOrderRefundRequestLineageSurvivesRoutingLoss(t *testing.T) {
	t.Parallel()
	for _, state := range []delivery.Kind{delivery.Succeeded, delivery.Uncertain, delivery.Sending} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			f := displacedRefundFixture(t, "paid")
			f.assign(t)
			notice := refundRequestOutcome(t, f, state)
			_, err := f.db.Exec(
				t.Context(),
				`DELETE FROM core.pass_payment_admins WHERE event_id='sandbox-festival' AND owner='alice'`,
			)
			require.NoError(t, err)
			routeRefundDue(t, f, 0)
			var retained int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT notification_id FROM core.order_refund_tasks`).Scan(&retained),
			)
			assert.Equal(t, notice.ID, retained)
			// Restart the service while routing is unavailable; recovery uses persisted evidence only.
			f.service = orders.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('sandbox-festival','alice')`,
			)
			require.NoError(t, err)
			routeRefundDue(t, f, 0)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET telegram_id=99001 WHERE id='alice'`)
			require.NoError(t, err)
			routeRefundDue(t, f, 0)
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('sandbox-festival','bob') ON CONFLICT DO NOTHING;
 UPDATE core.pass_bookings SET payment_admin='bob' WHERE event_id='sandbox-festival' AND owner='bob'`,
			)
			require.NoError(t, err)
			routeRefundDue(t, f, 1)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET payment_admin='alice' WHERE event_id='sandbox-festival' AND owner='bob'`,
			)
			require.NoError(t, err)
			routeRefundDue(t, f, 0)
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT notification_id FROM core.order_refund_tasks`).Scan(&retained),
			)
			assert.Equal(t, notice.ID, retained)
			var requests int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_notifications WHERE payload->>'kind'='refund_request' AND recipient='alice'`).
					Scan(&requests),
			)
			assert.Equal(t, 1, requests)
		})
	}
}

func TestOrderRefundRoutingRecoversOnlyKnownUnsentRequests(t *testing.T) {
	t.Parallel()
	for _, state := range []delivery.Kind{delivery.Cancelled, delivery.Rejected, delivery.Succeeded, delivery.Uncertain} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			f := displacedRefundFixture(t, "paid")
			f.assign(t)
			count, err := f.service.RouteRefunds(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			var id int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT notification_id FROM core.order_refund_tasks`).Scan(&id),
			)
			notice, ready, err := f.service.PrepareNotification(t.Context(), id)
			require.NoError(t, err)
			require.True(t, ready)
			if state != delivery.Cancelled {
				gate, beginErr := f.service.BeginNotification(
					t.Context(),
					delivery.Attempt{ID: id, Generation: notice.DeliveryAttempt},
				)
				require.NoError(t, beginErr)
				require.True(t, gate.Ready)
			}
			completion := orders.NotificationCompletion{
				ID:      id,
				Attempt: notice.DeliveryAttempt,
				Outcome: delivery.Outcome{Kind: state},
			}
			if state == delivery.Succeeded {
				completion.Text = "Refund request"
				completion.Outcome.MessageID = 101
			} else {
				completion.Outcome.Reason = "synthetic_failure"
			}
			require.NoError(t, f.service.CompleteNotification(t.Context(), completion))
			_, err = f.db.Exec(t.Context(), `UPDATE core.order_refund_tasks SET next_route_at=now()`)
			require.NoError(t, err)
			count, err = f.service.RouteRefunds(t.Context())
			require.NoError(t, err)
			if state == delivery.Cancelled || state == delivery.Rejected {
				assert.Equal(t, 1, count)
			} else {
				assert.Zero(t, count)
			}
			var notifications int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_notifications WHERE payload->>'kind'='refund_request'`).
					Scan(&notifications),
			)
			if state == delivery.Cancelled || state == delivery.Rejected {
				assert.Equal(t, 2, notifications)
			} else {
				assert.Equal(t, 1, notifications)
			}
		})
	}
}
