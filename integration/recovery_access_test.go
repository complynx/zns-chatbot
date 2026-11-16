package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryChatAccessLossRetainsMessageAndResumesAfterRestore(t *testing.T) {
	t.Parallel()
	for _, supported := range []bool{false, true} {
		name := "legacy"
		if supported {
			name = "recovery_v1"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRecoveryAccessRestore(t, supported)
		})
	}
}

func testRecoveryAccessRestore(t *testing.T, supported bool) {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	var runID string
	inaccessible, inboxCalls := false, 0
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		deny := inaccessible && r.Method == http.MethodGet &&
			(strings.Contains(r.URL.Path, "/runs/") || strings.HasSuffix(r.URL.Path, "/active-run"))
		currentRun := runID
		p.mu.Unlock()
		if deny {
			w.WriteHeader(http.StatusNotFound)
			respond(w, map[string]any{"error": map[string]string{"code": "not_found"}})
			return
		}
		if supported && r.URL.Path == "/v1/capabilities" {
			respondRecoveryCapabilities(w)
			return
		}
		switch r.URL.Path {
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			if currentRun == "" {
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
				return
			}
			respond(w, loom.ActiveRun{RunID: currentRun, Status: "accepted", NextAction: "send_message"})
		case "/v1/chats/retained-chat/messages":
			p.mu.Lock()
			inboxCalls++
			p.mu.Unlock()
			respond(w, loom.RecoveryReceipt{Accepted: true, Delivered: true, RunID: currentRun,
				MessageID: "restored-message", InputID: "restored-input", Sequence: 1,
				Status: "accepted", NextAction: "read_run"})
		default:
			p.agent(w, r)
		}
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 401)
	awaitLoomRun(t, h, first)
	h.stop(t)
	row, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	runID, inaccessible = row.RunID, true
	p.mu.Unlock()
	h.start(t)
	second := h.submit(t, 402)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, sent := range p.sent {
			if strings.Contains(sent.Text, "Access to the original conversation") {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	awaitRecoveryDeliverySettled(t, h, second)
	h.stop(t)
	pending, err := h.store.GetTask(t.Context(), second)
	require.NoError(t, err)
	require.NotEqual(t, "failed", pending.State)
	p.mu.Lock()
	inaccessible = false
	p.mu.Unlock()
	// Advance only this isolated fixture's parked jobs, without a one-minute sleep.
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
	h.await(t, second, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	if supported {
		require.Equal(t, 1, inboxCalls)
	} else {
		require.Len(t, p.inputs, 1)
	}
}

func TestRecoveryDiscoveryAccessAndUnknownAuthorizationRemainParked(t *testing.T) {
	t.Parallel()
	for _, blockedPath := range []string{
		"/v1/capabilities", "/v1/chats/default", "/v1/chats/retained-chat/active-run", "auth_unknown",
	} {
		t.Run(blockedPath, func(t *testing.T) {
			t.Parallel()
			testRecoveryDiscoveryAccess(t, blockedPath)
		})
	}
}

func testRecoveryDiscoveryAccess(t *testing.T, blockedPath string) {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	blocked, inboxCalls := false, 0
	var retained string
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if blocked && r.URL.Path == blockedPath {
			w.WriteHeader(http.StatusForbidden)
			respond(w, map[string]any{"error": map[string]string{"code": "forbidden"}})
			return
		}
		switch r.URL.Path {
		case "/v1/capabilities":
			respondRecoveryCapabilities(w)
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			located := loom.ActiveRun{Status: "idle", NextAction: "create_run"}
			if retained != "" {
				located = loom.ActiveRun{RunID: retained, Status: "accepted", NextAction: "send_message"}
			}
			respond(w, located)
		case "/v1/chats/retained-chat/messages":
			inboxCalls++
			respond(w, loom.RecoveryReceipt{Accepted: true, Delivered: true, RunID: retained,
				MessageID: "restored", Sequence: 1, Status: "accepted", NextAction: "read_run"})
		default:
			p.mu.Unlock()
			p.agent(w, r)
			p.mu.Lock()
		}
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 801)
	awaitLoomRun(t, h, first)
	h.stop(t)
	row, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	retained, blocked = row.RunID, true
	p.mu.Unlock()
	if blockedPath == "auth_unknown" {
		_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET state='auth_unknown' WHERE id=$1", first)
	} else {
		_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET state='failed' WHERE id=$1", first)
		require.NoError(t, err)
		_, err = h.store.Pool().Exec(t.Context(),
			"UPDATE shuttle_conversations SET active_task_id=NULL,active_run_id='' WHERE id=$1", row.ConversationID)
	}
	require.NoError(t, err)
	h.start(t)
	second := h.submitText(t, 802, "Please keep my original message")
	require.Eventually(t, func() bool {
		out, e := h.store.ListOutbox(t.Context(), second)
		return e == nil && len(out) > 0 && out[0].State == "delivered"
	}, 10*time.Second, 20*time.Millisecond)
	awaitRecoveryDeliverySettled(t, h, second)
	h.stop(t)
	row, err = h.store.GetTask(t.Context(), second)
	require.NoError(t, err)
	require.NotEqual(t, "failed", row.State)
	p.mu.Lock()
	require.Zero(t, inboxCalls)
	blocked = false
	p.mu.Unlock()
	if blockedPath == "auth_unknown" {
		_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET state='running' WHERE id=$1", first)
		require.NoError(t, err)
	}
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
	require.Eventually(t, func() bool {
		current, readErr := h.store.GetTask(t.Context(), second)
		return readErr == nil && strings.Contains(string(current.Snapshot), `"delivered": true`)
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, inboxCalls)
	require.Len(t, p.createdInputs, 1)
}
