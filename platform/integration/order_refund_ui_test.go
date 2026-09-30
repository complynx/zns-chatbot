package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func refundUIFixture(t *testing.T, f *fixture) orders.RefundTask {
	t.Helper()
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Key:     "refund-ui-order",
		Origin:  "manual",
		Choice:  orderChoice("shuttle"),
	})
	require.NoError(t, err)
	var id int64
	require.NoError(t, f.db.QueryRow(t.Context(), `INSERT INTO core.order_refund_tasks
	(order_id,event_id,owner,displacement_version,payment_attempt,amount_cents,extras_cents)
	VALUES($1,'sandbox-festival','alice',2,'paid-before-displacement',6500,'{"shuttle":6500}') RETURNING id`, order.ID).Scan(&id))
	task, err := f.b.API.Refund(t.Context(), "alice", id)
	require.NoError(t, err)
	return task
}

func assignRefundAmbassador(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at)
	VALUES('sandbox-festival','2100-01-01Z') ON CONFLICT DO NOTHING;
	INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('sandbox-festival','bob') ON CONFLICT DO NOTHING;
	INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
	VALUES('sandbox-festival','alice',1,'paid','leader','solo','bob',clock_timestamp(),clock_timestamp(),100)
	ON CONFLICT(event_id,owner) DO UPDATE SET payment_admin='bob'`)
	require.NoError(t, err)
}

func TestRefundTypedTransportRequiresManualConfirmation(t *testing.T) {
	t.Parallel()
	for _, direct := range []bool{false, true} {
		name := "http"
		if direct {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			task := refundUIFixture(t, f)
			client := f.b.API
			if direct {
				client = localOrderClient(t, f)
			}
			page, err := client.RefundTasks(t.Context(), "alice", task.EventID, 0)
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.Equal(t, "ambassador_missing", page.Items[0].RoutingReason)
			assert.False(t, page.Items[0].CanConfirm)
			assignRefundAmbassador(t, f)
			command := orders.RefundConfirmation{ID: task.ID, Version: task.Version, Key: "manual-refund"}
			_, err = client.ConfirmRefund(t.Context(), "bob", command)
			requireCode(t, err, "refund_invalid")
			command.Confirmed = true
			_, err = client.ConfirmRefund(t.Context(), "alice", command)
			requireCode(t, err, "forbidden")
			completed, err := client.ConfirmRefund(t.Context(), "bob", command)
			require.NoError(t, err)
			assert.Equal(t, "refunded", completed.State)
			replay, err := client.ConfirmRefund(t.Context(), "bob", command)
			require.NoError(t, err)
			assert.Equal(t, completed.Version, replay.Version)
			_, err = f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET payment_admin='alice' WHERE owner='alice'`)
			require.NoError(t, err)
			_, err = client.ConfirmRefund(t.Context(), "bob", command)
			requireCode(t, err, "forbidden")
		})
	}
}

func TestRefundTelegramManualCompletionLocales(t *testing.T) {
	t.Parallel()
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			task := refundUIFixture(t, f)
			for _, owner := range []string{"alice", "bob"} {
				_, err := f.b.API.SetLanguage(t.Context(), owner, locale, false)
				require.NoError(t, err)
			}
			handleVisible(t, f.b, message(400, 101, "/orders"))
			missing, err := i18n.Translate(locale, i18n.RefundUnassigned, nil)
			require.NoError(t, err)
			assert.Contains(t, orderChatText(t, f, 101), missing)
			assignRefundAmbassador(t, f)
			handleVisible(t, f.b, message(401, 202, "/orders"))
			label, err := i18n.Translate(locale, i18n.RefundConfirm, nil)
			require.NoError(t, err)
			assert.NotContains(t, orderChatText(t, f, 101), label)
			stale := orderClick(t, f, 202, 402, label)
			_, err = f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET payment_admin='alice' WHERE owner='alice'`)
			require.NoError(t, err)
			handleVisible(t, f.b, stale)
			current, err := f.b.API.Refund(t.Context(), "alice", task.ID)
			require.NoError(t, err)
			assert.Equal(t, "pending", current.State)
			assert.NotContains(t, orderChatText(t, f, 202), label)
			assignRefundAmbassador(t, f)
			handleVisible(t, f.b, message(403, 202, "/orders"))
			confirm := orderClick(t, f, 202, 404, label)
			handleVisible(t, f.b, confirm)
			handleVisible(t, f.b, confirm)
			current, err = f.b.API.Refund(t.Context(), "alice", task.ID)
			require.NoError(t, err)
			assert.Equal(t, "refunded", current.State)
			assert.Equal(t, "bob", current.RefundedBy)
			handleVisible(t, f.b, message(405, 101, "/orders"))
			completed, err := i18n.Translate(locale, i18n.RefundCompleted, nil)
			require.NoError(t, err)
			assert.Contains(t, orderChatText(t, f, 101), completed)
			assert.NotContains(t, orderChatText(t, f, 202), label)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_refund_audit WHERE task_id=$1 AND action='refunded'`, task.ID).
					Scan(&count),
			)
			assert.Equal(t, 1, count)
		})
	}
}

func TestRefundListTransportPaginationAndVisibility(t *testing.T) {
	t.Parallel()
	f := setup(t)
	task := refundUIFixture(t, f)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.order_refund_tasks
	(order_id,event_id,owner,displacement_version,payment_attempt,amount_cents,extras_cents)
	SELECT $1,'sandbox-festival','alice',n,'paid-before-displacement',6500,'{"shuttle":6500}'
	FROM generate_series(3,27) n`, task.OrderID)
	require.NoError(t, err)
	first, err := f.b.API.RefundTasks(t.Context(), "alice", task.EventID, 0)
	require.NoError(t, err)
	require.Len(t, first.Items, 25)
	require.Positive(t, first.NextBefore)
	second, err := f.b.API.RefundTasks(t.Context(), "alice", task.EventID, first.NextBefore)
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	assert.Equal(t, task.ID, second.Items[0].ID)
	assert.Zero(t, second.NextBefore)
	other, err := f.b.API.RefundTasks(t.Context(), "bob", task.EventID, 0)
	require.NoError(t, err)
	assert.Empty(t, other.Items, "an order payment administrator is not the current pass ambassador")
}
