package integration_test

import (
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

// Real task storage/queue and HTTP uncertainty; Loom's paid/external-effect safety
// is exercised in the pinned upstream PR, not reimplemented by this peer.
func TestConversationRecoveryRetainsOrderedMessagesAcrossRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	var bodies = map[string]string{}
	receipts := map[string]loom.RecoveryReceipt{}
	var order []string
	messageCalls := 0
	blocked, lose := true, true
	var runID string
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			respond(
				w,
				map[string]any{
					"conversation_recovery": map[string]any{"version": 1},
					"output_validation":     map[string]any{"version": 1, "disallow_custom_emoji": true},
				},
			)
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			p.mu.Lock()
			defer p.mu.Unlock()
			if runID == "" {
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
				return
			}
			respond(w, loom.ActiveRun{RunID: runID, Status: "accepted", NextAction: "send_message"})
		case "/v1/chats/retained-chat/messages":
			p.mu.Lock()
			defer p.mu.Unlock()
			messageCalls++
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			key := r.Header.Get("Idempotency-Key")
			recordRecoveryRequest(t, bodies, &order, key, string(body))
			if lose {
				lose = false
				disconnect(w)
				return
			}
			if blocked {
				respond(
					w,
					loom.RecoveryReceipt{
						Accepted:   true,
						MessageID:  "saved-" + key,
						Sequence:   int64(len(order)),
						Status:     "blocked",
						Reason:     "credits_exhausted",
						NextAction: "fund_wallet_then_retry",
					},
				)
				return
			}
			receipt, exists := receipts[key]
			if !exists {
				runID = "recovered-" + key
				p.runs[runID] = loom.Run{
					ID:            runID,
					ChatID:        "retained-chat",
					AuthSessionID: "session",
					Status:        "completed",
					Result:        "Checked " + key,
					Interaction:   loom.Interaction{ReplyMode: loom.ReplyModeAgent},
				}
				receipt = loom.RecoveryReceipt{
					Accepted:   true,
					Delivered:  true,
					MessageID:  "saved-" + key,
					Sequence:   int64(len(order)),
					RunID:      runID,
					Status:     "queued",
					NextAction: "read_run",
				}
				receipts[key] = receipt
			}
			respond(w, receipt)
		default:
			p.agent(w, r)
		}
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submitText(t, 1, "First request")
	h.await(t, first, "completed")
	initial, err := h.store.GetTask(context.Background(), first)
	require.NoError(t, err)
	p.mu.Lock()
	runID = initial.RunID
	p.mu.Unlock()
	second := h.submitText(t, 2, "Check the previous result")
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(order) == 1 && !lose },
		15*time.Second,
		25*time.Millisecond,
	)
	require.Eventually(t, func() bool {
		row, e := h.store.GetTask(context.Background(), second)
		return e == nil && strings.Contains(string(row.Snapshot), "fund_wallet_then_retry")
	}, 15*time.Second, 25*time.Millisecond)
	h.stop(t)
	_, err = h.store.Pool().Exec(context.Background(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(
	 snapshot,'{recovery,retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		second, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
	require.NoError(t, err)
	p.mu.Lock()
	blocked = false
	before := messageCalls
	p.mu.Unlock()
	h.start(t)
	require.Never(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return messageCalls > before },
		300*time.Millisecond,
		25*time.Millisecond,
	)
	third := submitFile(t, h, 3, time.Now())
	h.await(t, second, "completed")
	h.await(t, third, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, []string{second.String(), third.String()}, order)
	require.Len(t, p.createdInputs, 1, "recovery must not fall back to a second POST /runs")
	require.Contains(t, bodies[second.String()], "Check the previous result")
	require.Contains(t, bodies[third.String()], "Attachment metadata")
	require.Contains(t, bodies[third.String()], "Посмотри этот документ")
	require.NotContains(t, bodies[third.String()], "private-telegram-file")
}

func recordRecoveryRequest(t *testing.T, bodies map[string]string, order *[]string, key, body string) {
	t.Helper()
	if prior, ok := bodies[key]; ok {
		assert.Equal(t, prior, body)
		return
	}
	bodies[key] = body
	*order = append(*order, key)
}

func TestConversationRecoveryReplacesRunAfterLocalPointerLoss(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	var predecessor string
	var staleLocator bool
	busyCreations := 0
	var savedBody string
	attempts := 0
	var delivered loom.RecoveryReceipt
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			respond(
				w,
				map[string]any{
					"conversation_recovery": map[string]any{"version": 1},
					"output_validation":     map[string]any{"version": 1, "disallow_custom_emoji": true},
				},
			)
		case "/v1/chats/default":
			respond(w, map[string]string{"id": "retained-chat"})
		case "/v1/chats/retained-chat/active-run":
			p.mu.Lock()
			defer p.mu.Unlock()
			if predecessor == "" || staleLocator {
				staleLocator = false
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
				return
			}
			respond(
				w,
				loom.ActiveRun{
					RunID:      predecessor,
					Status:     "blocked",
					Reason:     "context_unavailable",
					NextAction: "resolve_blocker_then_resume",
				},
			)
		case "/v1/chats/retained-chat/messages":
			p.mu.Lock()
			defer p.mu.Unlock()
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			attempts++
			if attempts == 1 {
				savedBody = string(body)
				var request loom.RecoverMessage
				assert.NoError(t, json.Unmarshal(body, &request))
				assert.Equal(t, predecessor, request.ReplaceRunID)
				cancelRecoveryPeerRun(p, predecessor)
				p.runs["successor"] = loom.Run{
					ID:            "successor",
					ChatID:        "retained-chat",
					AuthSessionID: "session",
					Status:        "completed",
					Result:        "Recovered result",
					Interaction:   loom.Interaction{ReplyMode: loom.ReplyModeAgent},
				}
				delivered = loom.RecoveryReceipt{
					Accepted:   true,
					Delivered:  true,
					MessageID:  "durable-message",
					Sequence:   2,
					RunID:      "successor",
					Status:     "queued",
					NextAction: "read_run",
				}
				disconnect(w)
				return
			}
			assert.Equal(t, savedBody, string(body))
			respond(w, delivered)
		case "/v1/runs":
			p.mu.Lock()
			busy := predecessor != ""
			if busy {
				busyCreations++
			}
			p.mu.Unlock()
			if busy {
				w.WriteHeader(http.StatusConflict)
				respond(w, map[string]any{"error": map[string]string{"code": "chat_busy"}})
				return
			}
			p.agent(w, r)
		default:
			p.agent(w, r)
		}
	}))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submitText(t, 1, "Start work")
	h.await(t, first, "running")
	require.Eventually(t, func() bool {
		row, e := h.store.GetTask(context.Background(), first)
		return e == nil && row.RunID != ""
	}, 15*time.Second, 25*time.Millisecond)
	initial, err := h.store.GetTask(context.Background(), first)
	require.NoError(t, err)
	h.stop(t)
	// Reproduce the persisted incident: local task failed, but Loom retains the run.
	_, err = h.store.Pool().Exec(context.Background(), `UPDATE shuttle_tasks SET state='failed' WHERE id=$1`, first)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(context.Background(), `UPDATE shuttle_conversations
		 SET active_task_id=NULL,active_run_id='' WHERE id=$1`, initial.ConversationID)
	require.NoError(t, err)
	p.mu.Lock()
	predecessor = initial.RunID
	staleLocator = true
	p.mu.Unlock()
	h.start(t)
	second := h.submitText(t, 2, "Continue and verify")
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		current, e := h.store.GetTask(context.Background(), second)
		assert.NoError(check, e)
		var snapshot struct {
			Recovery json.RawMessage `json:"recovery"`
		}
		_ = json.Unmarshal(current.Snapshot, &snapshot)
		assert.Equal(check, "completed", current.State, "run=%s recovery=%s", current.RunID, string(snapshot.Recovery))
	}, 15*time.Second, 25*time.Millisecond)
	recovered, err := h.store.GetTask(context.Background(), second)
	require.NoError(t, err)
	require.Equal(t, "successor", recovered.RunID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.GreaterOrEqual(t, attempts, 2)
	require.Equal(t, 1, busyCreations, "a stale idle locator must recover the typed chat_busy creation rejection")
	require.Len(t, p.createdInputs, 1)
}

// The replacement handler holds p.mu while fencing its predecessor.
func cancelRecoveryPeerRun(p *peers, id string) {
	for key, run := range p.runs {
		if run.ID == id {
			run.Status = "cancelled"
			p.runs[key] = run
		}
	}
}
