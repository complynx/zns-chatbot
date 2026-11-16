package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestRecoveryDiscoveryOutageReportsStatusAndRetainsInput(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	var priorRun string
	outage, messages := false, 0
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		switch r.URL.Path {
		case "/v1/capabilities":
			respondRecoveryCapabilities(w)
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			switch {
			case outage:
				w.WriteHeader(http.StatusNotFound)
			case priorRun == "":
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
			default:
				respond(w, loom.ActiveRun{RunID: priorRun, Status: "terminal", NextAction: "send_message"})
			}
		case "/v1/chats/retained-chat/messages":
			messages++
			p.runs["discovered"] = loom.Run{ID: "discovered", ChatID: "retained-chat", AuthSessionID: "session",
				Status: "completed", Result: "Recovered after discovery", Interaction: loom.Interaction{ReplyMode: "agent"}}
			respond(w, loom.RecoveryReceipt{Accepted: true, Delivered: true, MessageID: "message",
				Sequence: 1, RunID: "discovered", Status: "queued", NextAction: "read_run"})
		default:
			p.mu.Unlock()
			p.agent(w, r)
			return
		}
		p.mu.Unlock()
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 601)
	h.await(t, first, "completed")
	h.stop(t)
	row, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	priorRun, outage = row.RunID, true
	p.mu.Unlock()
	h.start(t)
	second := h.submit(t, 602)
	require.Eventually(t, func() bool {
		current, e := h.store.GetTask(t.Context(), second)
		return e == nil && strings.Contains(string(current.Snapshot), "discovery_retry_until")
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	beforeStatus, err := h.store.ListOutbox(t.Context(), second)
	require.NoError(t, err)
	require.Empty(t, beforeStatus, "an untyped proxy 404 must use the service retry window")
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot ||
	 jsonb_build_object('discovery_retry_until',$2::text) WHERE id=$1`,
		second, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
	require.NoError(t, err)
	h.start(t)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, sent := range p.sent {
			if strings.Contains(sent.Text, "agent service is unavailable") {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		var parked bool
		e := h.store.Pool().QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM river_job
		 WHERE args->>'task_id'=$1 AND state='scheduled' AND scheduled_at>now()+interval '40 seconds')`,
			second.String()).Scan(&parked)
		return e == nil && parked
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	p.mu.Lock()
	outage = false
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot-
	 'discovery_next_check' WHERE id=$1`, second)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
	h.await(t, second, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, messages)
	require.Len(t, p.createdInputs, 2)
	for _, sent := range p.sent {
		require.NotContains(t, sent.Text, "Access to the original conversation")
	}
}

func TestRecoverySupersededObserverIsSilentAndTransfersKeyboard(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	var predecessor, frozenBody string
	attempts := 0
	release := make(chan struct{})
	var receipt loom.RecoveryReceipt
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		switch r.URL.Path {
		case "/v1/capabilities":
			respondRecoveryCapabilities(w)
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			if predecessor == "" {
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
			} else {
				respond(w, loom.ActiveRun{RunID: predecessor, Status: "accepted",
					Reason: "budget_exhausted", NextAction: "ask_user_to_continue"})
			}
		case "/v1/chats/retained-chat/messages":
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			attempts++
			if attempts == 1 {
				frozenBody = string(body)
				for key, run := range p.runs {
					run.Status, run.Reason = "cancelled", "superseded"
					p.runs[key] = run
				}
				p.runs["successor"] = loom.Run{ID: "successor", ReplacesRunID: predecessor,
					ChatID: "retained-chat", AuthSessionID: "session", Status: "completed", Result: "Successor final",
					Interaction: loom.Interaction{ReplyMode: "agent"}}
				receipt = loom.RecoveryReceipt{Accepted: true, Delivered: true, MessageID: "continuation",
					Sequence: 1, RunID: "successor", Status: "queued", NextAction: "read_run"}
				p.mu.Unlock()
				disconnect(w)
				return
			}
			assert.Equal(t, frozenBody, string(body))
			p.mu.Unlock()
			select {
			case <-release:
				respond(w, receipt)
			case <-r.Context().Done():
			}
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
	first := h.submit(t, 603)
	awaitLoomRun(t, h, first)
	p.mu.Lock()
	run := p.runs[first.String()]
	run.Status, run.Reason, run.ChatID = "paused", "budget_exhausted", "retained-chat"
	p.runs[first.String()] = run
	predecessor = run.ID
	p.mu.Unlock()
	h.awaitReplies(t, 1)
	second := h.submitText(t, 604, "Continue")
	require.Eventually(t, func() bool {
		row, err := h.store.GetTask(t.Context(), first)
		return err == nil && strings.Contains(string(row.Snapshot), "superseded")
	}, 10*time.Second, 20*time.Millisecond)
	close(release)
	h.await(t, second, "completed")
	h.await(t, first, "closed")
	h.awaitReplies(t, 2)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 2)
	require.NotContains(t, strings.ToLower(p.sent[1].Text), "cancel")
	require.Equal(t, "Successor final", p.sent[1].Text)
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[1].ReplyMarkup)
	require.GreaterOrEqual(t, attempts, 2)
}
