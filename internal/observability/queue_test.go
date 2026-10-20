//nolint:testpackage // A controlled clock verifies exported cache semantics without cache-duration sleeps.
package observability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueueMetricsFixedPairsAgeAndFailureRecovery(t *testing.T) {
	t.Parallel()
	telemetry, err := New(context.Background(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(context.Background())) })
	now := time.Unix(1000, 0)
	rows := []QueueMetric{
		{Kind: "shuttle_task", State: "available", Count: 2, ScheduledAgeSeconds: 10},
		{Kind: "zinusya_pair_notification", State: "available", Count: 1, ScheduledAgeSeconds: 3},
		{Kind: "zinusya_order_notification", State: "scheduled", Count: 4, ScheduledAgeSeconds: 2},
		{Kind: "zinusya_avatar_delivery", State: "available", Count: 2, ScheduledAgeSeconds: 3},
		{Kind: "zinusya_massage_reminder", State: "available", Count: 3, ScheduledAgeSeconds: 1},
		{Kind: "zinusya_massage_specialist_notice", State: "retryable", Count: 5, ScheduledAgeSeconds: 2},
	}
	var calls int
	failed := false
	require.NoError(t, telemetry.observeQueue(func(context.Context) ([]QueueMetric, error) {
		calls++
		if failed {
			return nil, errors.New("private queue failure")
		}
		return rows, nil
	}, func() time.Time { return now }))
	body := taskMetricsBody(t, telemetry)
	require.Equal(t, 24, strings.Count(body, "\nzinusya_queue_due{"))
	require.Equal(t, 24, strings.Count(body, "\nzinusya_queue_oldest_scheduled_age_seconds{"))
	require.Contains(t, body, `zinusya_queue_due{kind="shuttle_task",state="available"} 2`)
	require.Contains(t, body, `zinusya_queue_due{kind="zinusya_pair_notification",state="available"} 1`)
	require.Contains(t, body, `zinusya_queue_due{kind="zinusya_payment_notification",state="retryable"} 0`)
	require.Contains(t, body, `zinusya_queue_due{kind="zinusya_order_notification",state="scheduled"} 4`)
	require.Contains(t, body, `zinusya_queue_due{kind="zinusya_avatar_delivery",state="available"} 2`)
	require.Contains(t, body, `zinusya_queue_due{kind="zinusya_massage_reminder",state="available"} 3`)
	require.Contains(t, body, `zinusya_queue_due{kind="zinusya_massage_specialist_notice",state="retryable"} 5`)
	rows[0].Count = 99
	now = now.Add(5 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, `zinusya_queue_due{kind="shuttle_task",state="available"} 2`)
	require.Contains(t, body, `zinusya_queue_oldest_scheduled_age_seconds{kind="shuttle_task",state="available"} 15`)
	require.Equal(t, 1, calls)
	failed = true
	now = now.Add(26 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_queue_collection_success 0\n")
	require.Contains(t, body, "\nzinusya_queue_last_success_timestamp_seconds 1000\n")
	require.NotContains(t, body, "\nzinusya_queue_due{")
	require.NotContains(t, body, "\nzinusya_queue_oldest_scheduled_age_seconds{")
	require.NotContains(t, body, "private queue failure")
	failed = false
	now = now.Add(29 * time.Second)
	require.Contains(t, taskMetricsBody(t, telemetry), "\nzinusya_queue_collection_success 0\n")
	require.Equal(t, 2, calls)
	now = now.Add(time.Second)
	rows = nil
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_queue_collection_success 1\n")
	require.Equal(t, 24, strings.Count(body, "\nzinusya_queue_due{"))
	require.NotContains(t, body, "} 99")
	require.Equal(t, 3, calls)
}

func TestQueueMetricsRejectUnknownAndDuplicatePairs(t *testing.T) {
	t.Parallel()
	valid := QueueMetric{Kind: "shuttle_task", State: "available", Count: 1}
	for name, rows := range map[string][]QueueMetric{
		"unknown kind":  {{Kind: "private-user-token", State: "available", Count: 1}},
		"unknown state": {{Kind: "shuttle_task", State: "private-user-token", Count: 1}},
		"duplicate":     {valid, valid},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			telemetry, err := New(context.Background(), Options{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(context.Background())) })
			require.NoError(
				t,
				telemetry.ObserveQueue(func(context.Context) ([]QueueMetric, error) { return rows, nil }),
			)
			body := taskMetricsBody(t, telemetry)
			require.Contains(t, body, "\nzinusya_queue_collection_success 0\n")
			require.NotContains(t, body, "\nzinusya_queue_due{")
			require.NotContains(t, body, "private-user-token")
		})
	}
}
