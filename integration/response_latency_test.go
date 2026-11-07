package integration_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResponseLatencyWaitsForConfirmedReceiptAndDoesNotReplayAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	entered, release := make(chan struct{}), make(chan struct{})
	p.sendEntered, p.sendRelease = entered, release
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	h := prepareHarness(t, p)
	telemetry := workerTelemetry(t)
	h.observer = telemetry
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	t.Cleanup(unblock)
	id := h.submit(t, 85001)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Telegram peer did not receive the response")
	}
	_, err := h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_tasks SET response_started_at=clock_timestamp()-interval '2 hours' WHERE id=$1", id)
	require.NoError(t, err)
	require.NotContains(t, workerMetrics(t, telemetry), "zinusya_response_confirmed_duration_seconds_count")
	unblock()
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	body := workerMetrics(t, telemetry)
	require.Contains(t, body, "\nzinusya_response_confirmed_duration_seconds_count 1\n")
	require.Contains(t, body, "zinusya_response_confirmed_duration_seconds_bucket{le=\"3600\"} 0\n")
	require.Contains(t, body, "zinusya_response_confirmed_duration_seconds_bucket{le=\"21600\"} 1\n")
	var completed time.Time
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", id).
			Scan(&completed),
	)
	fresh := workerTelemetry(t)
	h.observer = fresh
	h.start(t)
	require.Equal(t, id, h.submit(t, 85001))
	h.stop(t)
	var preserved time.Time
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", id).
			Scan(&preserved),
	)
	require.Equal(t, completed, preserved)
	require.NotContains(t, workerMetrics(t, fresh), "zinusya_response_confirmed_duration_seconds_count")
	p.mu.Lock()
	require.Len(t, p.sent, 1)
	p.mu.Unlock()
}

func TestResponseLatencyDoesNotCountUnknownTelegramReceipt(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"telegram", "database"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			checkUnknownResponseReceipt(t, source)
		})
	}
}

func checkUnknownResponseReceipt(t *testing.T, source string) {
	t.Helper()
	p := newPeers(t)
	p.loseSend = source == "telegram"
	h := prepareHarness(t, p)
	if source == "database" {
		_, err := h.store.Pool().Exec(t.Context(), `
CREATE FUNCTION reject_response_stamp() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'synthetic receipt persistence failure'; END $$;
CREATE TRIGGER reject_response_stamp BEFORE UPDATE OF response_delivered_at ON shuttle_tasks
FOR EACH ROW EXECUTE FUNCTION reject_response_stamp()`)
		require.NoError(t, err)
	}
	telemetry := workerTelemetry(t)
	h.observer = telemetry
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	id := h.submit(t, 85002)
	h.await(t, id, "delivery_unknown")
	h.awaitReplies(t, 1)
	h.stop(t)
	var delivered *time.Time
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT response_delivered_at FROM shuttle_tasks WHERE id=$1", id).
			Scan(&delivered),
	)
	require.Nil(t, delivered)
	require.NotContains(t, workerMetrics(t, telemetry), "zinusya_response_confirmed_duration_seconds_count")
	h.start(t)
	require.Equal(t, id, h.submit(t, 85002))
	h.stop(t)
	p.mu.Lock()
	require.Len(t, p.sent, 1)
	p.mu.Unlock()
	require.NotContains(t, workerMetrics(t, telemetry), "zinusya_response_confirmed_duration_seconds_count")
}
