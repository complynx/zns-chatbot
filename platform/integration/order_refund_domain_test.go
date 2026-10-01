package integration_test

import (
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

type refundFixture struct {
	db      *pgxpool.Pool
	service orders.Service
	command orders.Command
	late    orders.Order
}

func preparedRefundFixture(t *testing.T, state string) refundFixture {
	t.Helper()
	db := database(t)
	s := orders.Service{DB: db, Delivery: syntheticDeliverySettings()}
	_, err := db.Exec(t.Context(), `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1')`)
	require.NoError(t, err)
	create := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "create",
		Choice:  orderChoice("shuttle"),
	}
	early, err := s.Execute(t.Context(), "alice", create)
	require.NoError(t, err)
	late, err := s.Execute(t.Context(), "bob", create)
	require.NoError(t, err)
	// Persisted payment attempts can reach reconciliation in a different order after import.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.orders SET state='proof',attempt='early',attempt_at='2026-01-01Z' WHERE id=$1`,
		early.ID,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.orders SET state=$2,attempt='late',attempt_at='2026-01-02Z',payment_admin='bob' WHERE id=$1`,
		late.ID,
		state,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.order_capacity_slots SET reservation_id=$1,reservation_attempt_token='late',
 reservation_attempt_created_at='2026-01-02Z',reserved_at='2026-01-02Z' WHERE service='shuttle' AND seat=0`,
		late.ID,
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,price}','70')`)
	require.NoError(t, err)
	early.Attempt = "early"
	command := orderCommand("country", early)
	command.Country, command.PaymentAdmin = "be", "bob"
	return refundFixture{db: db, service: s, command: command, late: late}
}

func displacedRefundFixture(t *testing.T, state string) refundFixture {
	t.Helper()
	f := preparedRefundFixture(t, state)
	_, err := f.service.Execute(t.Context(), "alice", f.command)
	require.NoError(t, err)
	return f
}

func TestOrderRefundCaptureFailureRollsBackDisplacement(t *testing.T) {
	t.Parallel()
	f := preparedRefundFixture(t, "paid")
	_, err := f.db.Exec(
		t.Context(),
		`ALTER TABLE core.order_refund_tasks ADD CONSTRAINT synthetic_refund_failure CHECK(false)`,
	)
	require.NoError(t, err)
	_, err = f.service.Execute(t.Context(), "alice", f.command)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotErrorIs(t, err, core.ErrDatabaseSerialization)
	require.NotContains(t, err.Error(), "synthetic_refund_failure")
	var databaseError *pgconn.PgError
	require.NotErrorAs(t, err, &databaseError)
	listed, err := f.service.List(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, orders.Money(6500), listed[0].Choice.Extras["shuttle"])
	assert.Equal(t, f.late.Version, listed[0].Version)
	var holder string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT reservation_id FROM core.order_capacity_slots WHERE service='shuttle' AND seat=0`).
			Scan(&holder),
	)
	assert.Equal(t, f.late.ID, holder)
	page, err := f.service.RefundTasks(t.Context(), "bob", "sandbox-festival", 0)
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	_, err = f.db.Exec(t.Context(), `ALTER TABLE core.order_refund_tasks DROP CONSTRAINT synthetic_refund_failure`)
	require.NoError(t, err)
	_, err = f.service.Execute(t.Context(), "alice", f.command)
	require.NoError(t, err)
	assert.Equal(t, orders.Money(6500), f.task(t).Amount)
}

func (f refundFixture) task(t *testing.T) orders.RefundTask {
	t.Helper()
	page, err := f.service.RefundTasks(t.Context(), "bob", "sandbox-festival", 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	return page.Items[0]
}

func (f refundFixture) assign(t *testing.T) {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('sandbox-festival',now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('sandbox-festival','alice');
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
 VALUES('sandbox-festival','bob',1,'paid','leader','solo','alice',now(),now(),100)`,
	)
	require.NoError(t, err)
}

func TestOrderRefundPaidDisplacementSnapshotAndReplay(t *testing.T) {
	t.Parallel()
	f := displacedRefundFixture(t, "paid")
	task := f.task(t)
	assert.Equal(t, orders.Money(6500), task.Amount)
	assert.Equal(t, map[string]orders.Money{"shuttle": 6500}, task.Extras)
	assert.Equal(t, "late", task.PaymentAttempt)
	assert.Equal(t, "BYN", task.Currency)
	assert.Equal(t, "ambassador_missing", task.RoutingReason)
	assert.False(t, task.CanConfirm)
	_, err := f.service.Execute(t.Context(), "alice", f.command)
	require.NoError(t, err)
	assert.Equal(t, task.ID, f.task(t).ID)
	listed, err := f.service.List(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "paid", listed[0].State)
	assert.NotContains(t, listed[0].Choice.Extras, "shuttle")
	assert.Zero(t, listed[0].Choice.Total)
	assert.Equal(t, listed[0].Version, task.DisplacementVersion)
}

func TestOrderRefundProofDisplacementIsNotPaid(t *testing.T) {
	t.Parallel()
	f := displacedRefundFixture(t, "proof")
	page, err := f.service.RefundTasks(t.Context(), "bob", "sandbox-festival", 0)
	require.NoError(t, err)
	assert.Empty(t, page.Items)
}

func TestOrderRefundCurrentAmbassadorManualConfirmation(t *testing.T) {
	t.Parallel()
	f := displacedRefundFixture(t, "paid")
	f.assign(t)
	task, err := f.service.Refund(t.Context(), "alice", f.task(t).ID)
	require.NoError(t, err)
	assert.True(t, task.CanConfirm)
	command := orders.RefundConfirmation{ID: task.ID, Version: task.Version, Key: "refund-confirm", Confirmed: true}
	_, err = f.service.ConfirmRefund(t.Context(), "bob", command)
	requireCode(t, err, "forbidden")
	command.Confirmed = false
	_, err = f.service.ConfirmRefund(t.Context(), "alice", command)
	requireCode(t, err, "refund_invalid")
	command.Confirmed = true
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, confirmErr := f.service.ConfirmRefund(t.Context(), "alice", command); errors <- confirmErr })
	}
	wg.Wait()
	close(errors)
	for confirmErr := range errors {
		require.NoError(t, confirmErr)
	}
	completed, err := f.service.Refund(t.Context(), "bob", task.ID)
	require.NoError(t, err)
	assert.Equal(t, "refunded", completed.State)
	assert.Equal(t, "alice", completed.RefundedBy)
	assert.NotNil(t, completed.RefundedAt)
	assert.Equal(t, task.Amount, completed.Amount)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_refund_audit WHERE task_id=$1 AND action='refunded'`, task.ID).
			Scan(&count),
	)
	assert.Equal(t, 1, count)
	command.Key = "another-refund"
	_, err = f.service.ConfirmRefund(t.Context(), "alice", command)
	requireCode(t, err, "refund_already_completed")
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET payment_admin='bob' WHERE event_id='sandbox-festival'`,
	)
	require.NoError(t, err)
	command.Key = "refund-confirm"
	_, err = f.service.ConfirmRefund(t.Context(), "alice", command)
	requireCode(t, err, "forbidden")
}

func TestOrderRefundRoutingAndDeliveryRecheckAmbassador(t *testing.T) {
	t.Parallel()
	f := displacedRefundFixture(t, "paid")
	count, err := f.service.RouteRefunds(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	f.assign(t)
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_refund_tasks SET next_route_at=now()`)
	require.NoError(t, err)
	count, err = f.service.RouteRefunds(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	count, err = f.service.RouteRefunds(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	var id int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT notification_id FROM core.order_refund_tasks`).Scan(&id))
	notice, ready, err := f.service.PrepareNotification(t.Context(), id)
	require.NoError(t, err)
	require.True(t, ready)
	require.True(t, notice.Current)
	assert.Equal(t, "alice", notice.Recipient)
	require.NotNil(t, notice.Refund)
	assert.Equal(t, orders.Money(6500), notice.Refund.Amount)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET payment_admin='bob' WHERE event_id='sandbox-festival'`,
	)
	require.NoError(t, err)
	gate, err := f.service.BeginNotification(
		t.Context(),
		orders.NotificationAttempt{
			ID: id, Generation: notice.DeliveryAttempt,
			Wire: notificationTestWire(),
		},
	)
	require.NoError(t, err)
	assert.False(t, gate.Ready)
	assert.Equal(t, "notification_no_longer_current", gate.Reason)
}
