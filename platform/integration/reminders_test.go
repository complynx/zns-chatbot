package integration_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestRemindersQueueOnceUnderConcurrentScansAndClaims(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, _ := cashOrder(t, f, "due")
	service := orders.Service{DB: f.db}
	require.NoError(
		t,
		sandbox.ApplyOrderFixture(t.Context(), f.db, sandbox.OrderFixture{OrderID: order.ID, Age: 72 * time.Hour}),
	)
	fresh, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: order.EventID,
			Name:    "create",
			Key:     "fresh",
			Origin:  "manual",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	empty, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: order.EventID,
			Name:    "create",
			Key:     "empty",
			Origin:  "manual",
			Choice:  &orders.ChoiceInput{},
		},
	)
	require.NoError(t, err)
	require.NoError(
		t,
		sandbox.ApplyOrderFixture(t.Context(), f.db, sandbox.OrderFixture{OrderID: empty.ID, Age: 72 * time.Hour}),
	)
	var group sync.WaitGroup
	type result struct {
		count int
		err   error
	}
	results := make(chan result, 8)
	for range 8 {
		group.Go(func() {
			count, scanError := service.QueueDueReminders(t.Context(), orders.DefaultReminderAfter)
			results <- result{count, scanError}
		})
	}
	group.Wait()
	close(results)
	queued := 0
	for result := range results {
		require.NoError(t, result.err)
		queued += result.count
	}
	assert.Equal(t, 1, queued)
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.order_notifications WHERE payload->>'kind'='reminder'`).
			Scan(&id),
	)
	claims := make(chan result, 8)
	for range 8 {
		group.Go(func() {
			claimed, claimError := service.ClaimReminder(t.Context(), id)
			count := 0
			if claimed {
				count = 1
			}
			claims <- result{count, claimError}
		})
	}
	group.Wait()
	close(claims)
	successful := 0
	for result := range claims {
		require.NoError(t, result.err)
		successful += result.count
	}
	assert.Equal(t, 1, successful)
	require.NoError(t, service.CompleteNotification(t.Context(), id, "reminder_failed"))
	count, err := service.QueueDueReminders(t.Context(), orders.DefaultReminderAfter)
	require.NoError(t, err)
	assert.Zero(t, count, "failed attempt is not scheduled again")
	var freshClaimed, emptyClaimed bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT reminder_claimed_at IS NOT NULL FROM core.orders WHERE id=$1`, fresh.ID).
			Scan(&freshClaimed),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT reminder_claimed_at IS NOT NULL FROM core.orders WHERE id=$1`, empty.ID).
			Scan(&emptyClaimed),
	)
	assert.False(t, freshClaimed)
	assert.False(t, emptyClaimed)
}

func TestReminderRechecksPaymentAndZeroTotalAtDelivery(t *testing.T) {
	t.Parallel()
	for _, paid := range []bool{false, true} {
		t.Run(map[bool]string{false: "zero total", true: "paid"}[paid], func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			order, _ := cashOrder(t, f, "due")
			service := orders.Service{DB: f.db}
			require.NoError(
				t,
				sandbox.ApplyOrderFixture(
					t.Context(),
					f.db,
					sandbox.OrderFixture{OrderID: order.ID, Age: 72 * time.Hour},
				),
			)
			count, err := service.QueueDueReminders(t.Context(), orders.DefaultReminderAfter)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			if paid {
				_, err = f.b.API.ExecuteOrder(t.Context(), "bob", orderCommand("accept", order))
			} else {
				edit := orderCommand("edit", order)
				edit.Choice = &orders.ChoiceInput{}
				_, err = f.b.API.ExecuteOrder(t.Context(), "alice", edit)
			}
			require.NoError(t, err)
			var id int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT id FROM core.order_notifications WHERE payload->>'kind'='reminder'`).
					Scan(&id),
			)
			claimed, err := service.ClaimReminder(t.Context(), id)
			require.NoError(t, err)
			assert.False(t, claimed, "outdated reminder must not be delivered")
		})
	}
}

func TestFailedReminderIsNotAttemptedAgain(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, _ := cashOrder(t, f, "due")
	service := orders.Service{DB: f.db}
	require.NoError(
		t,
		sandbox.ApplyOrderFixture(t.Context(), f.db, sandbox.OrderFixture{OrderID: order.ID, Age: 72 * time.Hour}),
	)
	_, err := service.QueueDueReminders(t.Context(), orders.DefaultReminderAfter)
	require.NoError(t, err)
	post(t, f.fake.URL+"/lab/blocked", map[string]any{"user": 101, "blocked": true})
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	var failure string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT failure FROM core.order_notifications WHERE payload->>'kind'='reminder'`).
			Scan(&failure),
	)
	assert.Equal(t, "reminder_failed", failure)
	post(t, f.fake.URL+"/lab/blocked", map[string]any{"user": 101, "blocked": false})
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	assert.Empty(t, chatMessages(t, f, 101), "unblocking does not repeat a claimed reminder")
}
