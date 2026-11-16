package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryBusyCreationRetriesWhenDiscoverySelectsOrdinaryCreation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{
		"idle", "finished_legacy", "legacy_read_outage", "unsupported", "pending_messages", "no_run",
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			testRecoveryBusyOrdinaryCreation(t, mode)
		})
	}
}

func testRecoveryBusyOrdinaryCreation(t *testing.T, mode string) {
	t.Helper()
	p := newPeers(t)
	var bodies, keys []string
	busy := false
	noRun := false
	readOutage := mode == "legacy_read_outage"
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		switch r.URL.Path {
		case "/v1/capabilities":
			if busy && mode == "unsupported" {
				respond(w, map[string]any{"output_validation": map[string]any{
					"version": 1, "disallow_custom_emoji": true}})
			} else {
				respondRecoveryCapabilities(w)
			}
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			respond(w, busyRecoveryLocation(mode, busy, noRun))
		case "/v1/chats/retained-chat/messages":
			noRun = true
			w.WriteHeader(http.StatusConflict)
			respond(w, map[string]any{"error": map[string]string{"code": "no_run"}})
		case "/v1/runs/finished":
			if readOutage {
				w.WriteHeader(http.StatusNotFound)
				break
			}
			respond(w, loom.Run{ID: "finished", Status: "completed"})
		case "/v1/runs":
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			bodies, keys = append(bodies, string(body)), append(keys, r.Header.Get("Idempotency-Key"))
			if !busy || readOutage {
				busy = true
				p.mu.Unlock()
				w.WriteHeader(http.StatusConflict)
				respond(w, map[string]any{"error": map[string]string{"code": recoveryBusyCode(mode)}})
				return
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			p.mu.Unlock()
			p.agent(w, r)
			return
		default:
			p.mu.Unlock()
			p.agent(w, r)
			return
		}
		p.mu.Unlock()
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	id := h.submitText(t, 1001, "Keep the original request")
	if mode == "legacy_read_outage" {
		checkLegacyReadOutage(t, h, id, func() {
			p.mu.Lock()
			readOutage = false
			p.mu.Unlock()
		})
	}
	h.await(t, id, "completed")
	deliveredNamePrompt(t, h, id)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.GreaterOrEqual(t, len(bodies), 2)
	for n := range bodies {
		require.Equal(t, bodies[0], bodies[n])
		require.Equal(t, id.String(), keys[n])
	}
	require.Len(t, p.createdInputs, 1)
	if mode == "no_run" {
		require.True(t, noRun)
	}
}

func busyRecoveryLocation(mode string, busy, noRun bool) loom.ActiveRun {
	if busy && mode == "no_run" && !noRun {
		return loom.ActiveRun{RunID: "vanished", Status: "accepted", NextAction: "send_message"}
	}
	if busy && (mode == "finished_legacy" || mode == "legacy_read_outage") {
		return loom.ActiveRun{RunID: "finished", Status: "blocked", Reason: "run_order_unavailable",
			NextAction: "stop_run_then_create_run"}
	}
	return loom.ActiveRun{Status: "idle", NextAction: "create_run"}
}

func recoveryBusyCode(mode string) string {
	if mode == "pending_messages" {
		return "pending_conversation_messages"
	}
	return "chat_busy"
}

func checkLegacyReadOutage(t *testing.T, h *harness, id uuid.UUID, restore func()) {
	t.Helper()
	require.Eventually(t, func() bool {
		current, readErr := h.store.GetTask(t.Context(), id)
		return readErr == nil && strings.Contains(string(current.Snapshot), `"discovery_retry_until"`)
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot ||
	 jsonb_build_object('discovery_retry_until',now()-interval '1 second') WHERE id=$1`, id)
	require.NoError(t, err)
	h.start(t)
	require.Eventually(t, func() bool {
		rows, readErr := h.store.ListOutbox(t.Context(), id)
		return readErr == nil && len(rows) > 0 && rows[0].State == "delivered"
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	rows, err := h.store.ListOutbox(t.Context(), id)
	require.NoError(t, err)
	require.Contains(t, string(rows[0].Payload), "agent service is unavailable")
	require.NotContains(t, string(rows[0].Payload), "stop the previous run")
	restore()
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot-
	 'discovery_next_check' WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
}
