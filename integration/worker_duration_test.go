package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/observability"
)

type workerDurationSample struct {
	Count, Seconds float64
}

func workerTelemetry(t *testing.T) *observability.Telemetry {
	t.Helper()
	telemetry, err := observability.New(t.Context(), observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(context.Background())) })
	return telemetry
}

func workerMetrics(t *testing.T, telemetry *observability.Telemetry) string {
	t.Helper()
	w := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func workerSample(t *testing.T, body, kind, outcome string) workerDurationSample {
	t.Helper()
	labels := fmt.Sprintf(`{kind=%q,`, kind)
	if outcome != "" {
		labels = fmt.Sprintf(`{kind=%q,outcome=%q}`, kind, outcome)
	}
	var sample workerDurationSample
	for line := range strings.SplitSeq(body, "\n") {
		count := strings.HasPrefix(line, "zinusya_worker_attempt_duration_seconds_count"+labels)
		sum := strings.HasPrefix(line, "zinusya_worker_attempt_duration_seconds_sum"+labels)
		if !count && !sum {
			continue
		}
		fields := strings.Fields(line[strings.IndexByte(line, '}')+1:])
		require.NotEmpty(t, fields)
		value, err := strconv.ParseFloat(fields[0], 64)
		require.NoError(t, err)
		if count {
			sample.Count += value
		} else {
			sample.Seconds += value
		}
	}
	return sample
}

func TestWorkerDurationRecordsOnlyAfterCallbackReturnsIncludingBlockedPeer(t *testing.T) {
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
	id := h.submit(t, 84001)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach the owned Telegram peer gate")
	}
	started := time.Now()
	before := workerSample(t, workerMetrics(t, telemetry), "shuttle_task", "")
	time.Sleep(75 * time.Millisecond)
	blocked := workerSample(t, workerMetrics(t, telemetry), "shuttle_task", "")
	require.Equal(t, before, blocked, "an unfinished callback must not contribute an observation")
	held := time.Since(started)
	unblock()
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	body := workerMetrics(t, telemetry)
	after := workerSample(t, body, "shuttle_task", "")
	require.Greater(t, after.Count, before.Count)
	require.GreaterOrEqual(t, after.Seconds-before.Seconds, held.Seconds())
	require.Positive(t, workerSample(t, body, "shuttle_task", "returned").Count)
	require.Contains(t, body, `component="worker",outcome="success"`)
}

func TestWorkerDurationRecordsReturnedErrorAndTaskLeaseCleanup(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	_, err := h.store.Pool().Exec(t.Context(), `
CREATE FUNCTION corrupt_worker_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN NEW.snapshot := '"private-invalid-checkpoint"'::jsonb; RETURN NEW; END $$;
CREATE TRIGGER corrupt_worker_checkpoint BEFORE INSERT ON shuttle_tasks
FOR EACH ROW EXECUTE FUNCTION corrupt_worker_checkpoint()`)
	require.NoError(t, err)
	telemetry := workerTelemetry(t)
	h.observer = telemetry
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	id := h.submit(t, 84002)
	require.Eventually(t, func() bool {
		return workerSample(t, workerMetrics(t, telemetry), "shuttle_task", "error").Count > 0
	}, 5*time.Second, 10*time.Millisecond)
	h.stop(t)
	current, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.Empty(t, current.LeaseOwner)
	require.NotEqual(t, "completed", current.State)
	body := workerMetrics(t, telemetry)
	require.Positive(t, workerSample(t, body, "shuttle_task", "error").Seconds)
	require.NotContains(t, body, "private-invalid-checkpoint")
	require.Empty(t, sentNotifications(h.peers, 456))
}

func TestWorkerDurationRecordsSnoozeWhileIAMUnavailable(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.iamDown = true
	h := prepareHarness(t, p)
	telemetry := workerTelemetry(t)
	h.observer = telemetry
	h.start(t)
	t.Cleanup(func() { h.stop(t) })
	id := h.submit(t, 84003)
	require.Eventually(t, func() bool {
		return workerSample(t, workerMetrics(t, telemetry), "shuttle_task", "snoozed").Count > 0
	}, 5*time.Second, 10*time.Millisecond)
	h.stop(t)
	current, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.NotEqual(t, "completed", current.State)
	require.Empty(t, sentNotifications(p, 456))
}

func TestWorkerDurationNotificationReturnDoesNotMeanDeliverySuccess(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"delivered", "delivery_unknown"} {
		t.Run("payment-"+state, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			f := setupPayment(t, p)
			f.H.stop(t)
			_, id := pendingNotice(t, f, "duration-notice")
			p.mu.Lock()
			p.loseSend = state == "delivery_unknown"
			p.mu.Unlock()
			telemetry := workerTelemetry(t)
			f.H.observer = telemetry
			f.H.start(t)
			awaitNotification(t, f, id, state)
			f.H.stop(t)
			sample := workerSample(t, workerMetrics(t, telemetry), "zinusya_payment_notification", "returned")
			require.Positive(t, sample.Count)
			require.Positive(t, sample.Seconds)
			require.Len(t, sentNotifications(p, 11003), 1)
		})
	}
	t.Run("pair-delivered", func(t *testing.T) {
		t.Parallel()
		f := setupPair(t)
		f.H.stop(t)
		id := pairPendingNotice(t, f)
		telemetry := workerTelemetry(t)
		f.H.observer = telemetry
		f.H.start(t)
		awaitPairNotice(t, f, id, "delivered")
		f.H.stop(t)
		sample := workerSample(t, workerMetrics(t, telemetry), "zinusya_pair_notification", "returned")
		require.Positive(t, sample.Count)
		require.Positive(t, sample.Seconds)
		require.Len(t, sentNotifications(f.H.peers, 12003), 1)
	})
}
