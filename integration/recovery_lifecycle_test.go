package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryDeliveredReceiptSurvivesReadOutageAndAuthorizationWait(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	var runID string
	outage := false
	pausedLocator := false
	messageCalls, locates, failedReads := 0, 0, 0
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		switch r.URL.Path {
		case "/v1/capabilities":
			p.mu.Unlock()
			respondRecoveryCapabilities(w)
			return
		case "/v1/chats/default":
			p.mu.Unlock()
			respond(w, map[string]string{"id": "retained-chat"})
			return
		case "/v1/chats/retained-chat/active-run":
			locates++
			if pausedLocator {
				respond(w, loom.ActiveRun{RunID: runID, Status: "accepted", Reason: "step_limit",
					NextAction: "ask_user_to_continue"})
				p.mu.Unlock()
				return
			}
			if runID == "" {
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
			} else {
				respond(w, loom.ActiveRun{RunID: runID, Status: "accepted", NextAction: "send_message"})
			}
			p.mu.Unlock()
			return
		case "/v1/chats/retained-chat/messages":
			messageCalls++
			outage = true
			respond(w, loom.RecoveryReceipt{Accepted: true, Delivered: true, MessageID: "retained",
				Sequence: 1, RunID: runID, InputID: "recovered-input", Status: "running", NextAction: "read_run"})
			p.mu.Unlock()
			return
		}
		if outage && r.URL.Path == "/v1/runs/"+runID {
			failedReads++
			p.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			return
		}
		p.mu.Unlock()
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submitText(t, 101, "Start")
	awaitLoomRun(t, h, first)
	h.stop(t)
	initial, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	// The former local observer is gone, while the run and session remain live.
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET state='failed' WHERE id=$1`, first)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_conversations SET active_task_id=NULL,
	 active_run_id='' WHERE id=$1`, initial.ConversationID)
	require.NoError(t, err)
	p.mu.Lock()
	runID = initial.RunID
	p.mu.Unlock()
	h.start(t)
	second := h.submitText(t, 102, "Continue")
	require.Eventually(t, func() bool {
		row, e := h.store.GetTask(t.Context(), second)
		var saved struct {
			Recovery struct {
				Receipt loom.RecoveryReceipt `json:"receipt"`
			} `json:"recovery"`
		}
		return e == nil && json.Unmarshal(row.Snapshot, &saved) == nil && saved.Recovery.Receipt.Delivered
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	// A pinned run ID is not proof of observer attachment. Restore a legacy
	// checkpoint whose receipt is durable but session handoff is still pending.
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET route='new_run',run_id=$2 WHERE id=$1`,
		second, runID)
	require.NoError(t, err)
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot ||
	 jsonb_build_object('run_read_retry_until',$2::text) WHERE id=$1`, second, past)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(
	 snapshot,'{recovery,retry_until}',to_jsonb($2::text)) WHERE id=$1`, second, past)
	require.NoError(t, err)
	p.mu.Lock()
	beforeLocates := locates
	beforeReads := failedReads
	pausedLocator = true
	p.mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return locates > beforeLocates && failedReads > beforeReads
	}, 10*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		rows, readErr := h.store.ListOutbox(t.Context(), second)
		for _, row := range rows {
			if strings.Contains(string(row.Payload), "agent service is unavailable") {
				return readErr == nil
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	p.mu.Lock()
	readsAfterLocate := failedReads
	p.mu.Unlock()
	require.Never(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return failedReads > readsAfterLocate
	}, 250*time.Millisecond, 20*time.Millisecond)
	h.stop(t)
	p.mu.Lock()
	outage = false
	require.Equal(t, 1, messageCalls, "delivered receipt must never replay the inbox mutation")
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot-
	 'run_read_next_check' WHERE id=$1`, second)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
	h.await(t, second, "running")
	h.stop(t)
	attached, err := h.store.GetTask(t.Context(), second)
	require.NoError(t, err)
	var checkpoint struct {
		Recovery struct {
			Observer string `json:"observer_run_id"`
		} `json:"recovery"`
	}
	require.NoError(t, json.Unmarshal(attached.Snapshot, &checkpoint))
	require.Equal(t, runID, checkpoint.Recovery.Observer)
	p.mu.Lock()
	run := p.runs[first.String()]
	run.Status, run.Reason = "waiting_auth", "recovery_authorization_required"
	p.runs[first.String()] = run
	p.mu.Unlock()
	h.start(t)
	h.await(t, second, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Equal(t, []int64{1}, p.reauthorizationVersions)
	require.Len(t, p.createdInputs, 1)
	for _, sent := range p.sent {
		require.NotContains(t, sent.Text, "Send another message")
	}
}

func respondRecoveryCapabilities(w http.ResponseWriter) {
	respond(w, map[string]any{"conversation_recovery": map[string]any{"version": 1},
		"output_validation": map[string]any{"version": 1, "disallow_custom_emoji": true}})
}

func TestRecoveryPreAcceptanceFallbackKeepsOrdinaryCreation(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{
		"other_actor", "run_order_unavailable", "terminal", "model_not_allowed", "authorization_required",
	} {
		t.Run(reason, func(t *testing.T) { t.Parallel(); testRecoveryCreationFallback(t, reason) })
	}
}

func testRecoveryCreationFallback(t *testing.T, reason string) {
	t.Helper()
	p := newPeers(t)
	var firstRun string
	inboxCalls := 0
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (reason == "terminal" || reason == "model_not_allowed" || reason == "authorization_required") &&
			r.URL.Path == "/v1/runs" && r.Method == http.MethodPost {
			var raw bytes.Buffer
			r.Body = io.NopCloser(io.TeeReader(r.Body, &raw))
			p.agent(w, r)
			var creation loom.CreateRunRequest
			assert.NoError(t, json.Unmarshal(raw.Bytes(), &creation))
			assert.Equal(t, &loom.OutputPolicy{DisallowCustomEmoji: true}, creation.OutputPolicy)
			assert.Equal(t, &loom.Discovery{ExpandServices: []string{"zinusya"}}, creation.Discovery)
			assert.NotNil(t, creation.Interaction.Reactions)
			return
		}
		if r.URL.Path == "/v1/chats/retained-chat/messages" {
			p.mu.Lock()
			inboxCalls++
			p.mu.Unlock()
			respond(w, loom.RecoveryReceipt{Accepted: true, MessageID: "unexpected-inbox", Sequence: 1,
				Status: "blocked", Reason: "authorization_required", NextAction: "reauthorize_session"})
			return
		}
		if r.URL.Path == "/v1/capabilities" {
			respondRecoveryCapabilities(w)
			return
		}
		if r.URL.Path == "/v1/chats/default" {
			respond(w, map[string]string{"id": "retained-chat"})
			return
		}
		if r.URL.Path == "/v1/chats/retained-chat/active-run" {
			p.mu.Lock()
			defer p.mu.Unlock()
			switch {
			case firstRun == "":
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
			case reason == "other_actor":
				w.WriteHeader(http.StatusNotFound)
				respond(w, map[string]any{"error": map[string]string{"code": "not_found"}})
			case reason == "terminal":
				respond(w, loom.ActiveRun{RunID: firstRun, Status: "terminal", NextAction: "send_message"})
			case reason == "model_not_allowed":
				respond(w, loom.ActiveRun{RunID: firstRun, Status: "blocked", Reason: reason,
					NextAction: "select_allowed_model_or_restore_access"})
			case reason == "authorization_required":
				respond(w, loom.ActiveRun{RunID: firstRun, Status: "blocked", Reason: reason,
					NextAction: "reauthorize_session"})
			default:
				respond(w, loom.ActiveRun{RunID: firstRun, Status: "blocked",
					Reason: reason, NextAction: "stop_run_then_create_run"})
			}
			return
		}
		p.agent(w, r)
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submitText(t, 201, "First")
	h.await(t, first, "completed")
	row, err := h.store.GetTask(context.Background(), first)
	require.NoError(t, err)
	p.mu.Lock()
	firstRun = row.RunID
	if reason == "authorization_required" {
		p.loseReauthorization = true
	}
	p.mu.Unlock()
	if reason == "authorization_required" {
		_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,
		 '{session,expires_at}',to_jsonb($2::text)) WHERE id=$1`,
			first, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano))
		require.NoError(t, err)
	}
	second := h.submitText(t, 202, "New round")
	h.await(t, second, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 2)
	require.Zero(t, inboxCalls, "known-terminal requests need current creation settings")
	require.Zero(t, p.sessionReauthorizations, "new rounds must not renew a terminal run's session")
}
