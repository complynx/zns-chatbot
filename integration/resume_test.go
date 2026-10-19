package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func pauseMockRun(t *testing.T, h *harness, id uuid.UUID, reason string) {
	t.Helper()
	current, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	for key, run := range h.peers.runs {
		if run.ID == current.RunID {
			run.Status = "paused"
			run.Reason = reason
			run.Version++
			h.peers.runs[key] = run
		}
	}
	h.peers.runGets = 0
}
func completeMockRun(p *peers) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, run := range p.runs {
		run.Status = "completed"
		run.Result = "Recovered original run"
		p.runs[key] = run
	}
}

func TestShutdownResumeRetainsKeyThroughUnknownReceiptAndRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	p.dropResume = true
	p.resumeBlocked = true
	h := setup(t, p)
	id := h.submit(t, 101)
	awaitLoomRun(t, h, id)
	initial, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	pauseMockRun(t, h, id, "shutdown")
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.resumeKeys) > 0 },
		5*time.Second,
		20*time.Millisecond,
	)
	h.stop(t)
	h.start(t)
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.resumeKeys) > 1 },
		5*time.Second,
		20*time.Millisecond,
	)
	h.await(t, id, "awaiting_service")
	p.mu.Lock()
	p.resumeBlocked = false
	p.mu.Unlock()
	awaitLoomRun(t, h, id)
	current, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, initial.RunID, current.RunID)
	completeMockRun(p)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.resumeEffects)
	require.Len(t, p.runs, 1)
	require.Len(t, p.sent, 1)
	for _, key := range p.resumeKeys {
		require.Equal(t, p.resumeKeys[0], key)
	}
}

func TestLaterShutdownUsesNewResumeOperationForSameRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	p.loseResume = true
	h := setup(t, p)
	id := h.submit(t, 102)
	awaitLoomRun(t, h, id)
	for effect := 1; effect <= 2; effect++ {
		pauseMockRun(t, h, id, "shutdown")
		require.Eventually(
			t,
			func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.resumeEffects == effect },
			5*time.Second,
			20*time.Millisecond,
		)
		awaitLoomRun(t, h, id)
		if effect == 1 {
			h.stop(t)
			h.start(t)
			awaitLoomRun(t, h, id)
		}
	}
	completeMockRun(p)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.resumeKeys, 2)
	require.NotEqual(t, p.resumeKeys[0], p.resumeKeys[1])
	require.Len(t, p.sent, 1)
}

func TestExplicitOrUncertainPauseIsNotAutomaticallyResumed(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"client_pause", "provider_retry_exhausted", "authorization_required", "module_discovery_unavailable"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.steering = true
			h := setup(t, p)
			id := h.submit(t, 103)
			awaitLoomRun(t, h, id)
			pauseMockRun(t, h, id, reason)
			require.Eventually(
				t,
				func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.runGets >= 3 },
				5*time.Second,
				20*time.Millisecond,
			)
			h.stop(t)
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Empty(t, p.resumeKeys)
			require.Empty(t, p.sent)
			require.Len(t, p.runs, 1)
		})
	}
}
