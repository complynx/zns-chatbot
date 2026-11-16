package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
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
	for _, recovered := range []bool{false, true} {
		name := "ordinary"
		if recovered {
			name = "recovered"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testLaterShutdownResume(t, recovered)
		})
	}
}

func testLaterShutdownResume(t *testing.T, recovered bool) {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	p.loseResume = true
	var h *harness
	var id uuid.UUID
	if recovered {
		h, id = recoveredMockObserver(t, p)
	} else {
		h = setup(t, p)
		id = h.submit(t, 102)
		awaitLoomRun(t, h, id)
	}
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

func recoveredMockObserver(t *testing.T, p *peers) (*harness, uuid.UUID) {
	t.Helper()
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		runID := ""
		for _, run := range p.runs {
			runID = run.ID
		}
		located := loom.ActiveRun{Status: "idle", NextAction: "create_run"}
		if runID != "" {
			located = loom.ActiveRun{RunID: runID, Status: "queued", NextAction: "send_message"}
		}
		if respondRecoveryDiscovery(w, r, located) {
			p.mu.Unlock()
			return
		}
		if r.URL.Path == "/v1/chats/retained-chat/messages" {
			respond(w, loom.RecoveryReceipt{Accepted: true, Delivered: true, MessageID: "resumed-message",
				RunID: runID, Sequence: 1, Status: "queued", NextAction: "read_run"})
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 102)
	awaitLoomRun(t, h, first)
	h.stop(t)
	row, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET state='failed' WHERE id=$1", first)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_conversations SET active_task_id=NULL,
	 active_run_id='' WHERE id=$1`, row.ConversationID)
	require.NoError(t, err)
	h.start(t)
	id := h.submitText(t, 103, "Continue the retained run")
	awaitLoomRun(t, h, id)
	return h, id
}

func TestExplicitOrUncertainPauseIsNotAutomaticallyResumed(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"client_pause", "provider_retry_exhausted", "authorization_required",
		"module_discovery_unavailable"} {
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
