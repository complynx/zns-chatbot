package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryNoRunRediscoveryCreatesOriginalMessageOnce(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	inboxCalls := 0
	var creationKey string
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		located := loom.ActiveRun{RunID: "vanished", Status: "accepted", NextAction: "send_message"}
		if inboxCalls > 0 {
			located = loom.ActiveRun{Status: "idle", NextAction: "create_run"}
		}
		if respondRecoveryDiscovery(w, r, located) {
			p.mu.Unlock()
			return
		}
		if r.URL.Path == "/v1/chats/retained-chat/messages" {
			inboxCalls++
			p.mu.Unlock()
			w.WriteHeader(http.StatusConflict)
			respond(w, map[string]any{"error": map[string]string{"code": "no_run"}})
			return
		}
		if r.URL.Path == "/v1/runs" && r.Method == http.MethodPost {
			creationKey = r.Header.Get("Idempotency-Key")
		}
		p.mu.Unlock()
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	id := h.submitText(t, 1241, "Original message after a disappearing run")
	h.await(t, id, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, inboxCalls)
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], "Original message after a disappearing run")
	require.Len(t, p.createAttempts, 1)
	require.Equal(t, id.String(), creationKey)
}

func TestRecoveryInboxNotFoundKeepsOnlyUncertainSubmissionPending(t *testing.T) {
	t.Parallel()
	for _, firstFailure := range []string{"not_found", "unknown", "forbidden"} {
		t.Run(firstFailure, func(t *testing.T) { t.Parallel(); testRecoveryInboxNotFound(t, firstFailure) })
	}
}

func testRecoveryInboxNotFound(t *testing.T, firstFailure string) {
	t.Helper()
	p := newPeers(t)
	calls := 0
	lostResponse := firstFailure == "unknown"
	checkSubmission := func(string) {}
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		located := loom.ActiveRun{RunID: "vanished", Status: "accepted", NextAction: "send_message"}
		if calls > 0 && !lostResponse {
			located = loom.ActiveRun{Status: "idle", NextAction: "create_run"}
		}
		if respondRecoveryDiscovery(w, r, located) {
			p.mu.Unlock()
			return
		}
		if r.URL.Path == "/v1/chats/retained-chat/messages" {
			checkSubmission(r.Header.Get("Idempotency-Key"))
			calls++
			lose := lostResponse && calls == 1
			forbidden := firstFailure == "forbidden" && calls == 1
			p.mu.Unlock()
			if lose {
				disconnect(w)
				return
			}
			if forbidden {
				w.WriteHeader(http.StatusForbidden)
				respond(w, map[string]any{"error": map[string]string{"code": "forbidden"}})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			respond(w, map[string]any{"error": map[string]string{"code": "not_found"}})
			return
		}
		p.mu.Unlock()
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	p.mu.Lock()
	checkSubmission = func(key string) {
		var unsubmitted bool
		err := h.store.Pool().QueryRow(t.Context(), `SELECT
		 COALESCE((snapshot#>>'{recovery,unsubmitted}')::boolean,false) FROM shuttle_tasks WHERE id=$1::uuid`, key).
			Scan(&unsubmitted)
		if err != nil {
			t.Errorf("read durable submission intent: %v", err)
			return
		}
		assert.False(t, unsubmitted, "submission intent must be durable before any HTTP attempt")
	}
	p.mu.Unlock()
	first := h.submitText(t, 1243, "Retain the outcome of this message")
	if firstFailure == "forbidden" {
		h.await(t, first, "awaiting_service")
		awaitRecoveryDeliverySettled(t, h, first)
		h.stop(t)
		_, err := h.store.Pool().Exec(t.Context(), `UPDATE river_job SET scheduled_at=now()
		 WHERE args->>'task_id'=$1 AND state='scheduled'`, first.String())
		require.NoError(t, err)
		h.start(t)
	}
	if !lostResponse {
		h.await(t, first, "failed")
		awaitRecoveryDeliverySettled(t, h, first)
		later := h.submitText(t, 1244, "A subsequent request can proceed")
		h.await(t, later, "completed")
		p.mu.Lock()
		defer p.mu.Unlock()
		require.Equal(t, map[string]int{"not_found": 1, "forbidden": 2}[firstFailure], calls)
		require.Len(t, p.createdInputs, 1)
		return
	}
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return calls >= 2
	}, 10*time.Second, 20*time.Millisecond)
	awaitRecoveryDeliverySettled(t, h, first)
	h.stop(t)
	current, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	require.Equal(t, "awaiting_service", current.State)
	require.NotContains(t, string(current.Snapshot), `"rejected": true`)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.createdInputs, "an unknown earlier submission cannot fall back to paid creation")
}
