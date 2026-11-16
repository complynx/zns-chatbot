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
	awaitRecoveryDeliverySettled(t, h, second)
	h.stop(t)
	retained, err := h.store.GetTask(t.Context(), second)
	require.NoError(t, err)
	require.NoError(t, h.store.RevokeUser(t.Context(), retained.UserID, true))
	p.mu.Lock()
	beforeDenied := messageCalls
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE river_job SET state='available',scheduled_at=now()
	 WHERE args->>'task_id'=$1 AND state IN ('available','scheduled','retryable')`, second.String())
	require.NoError(t, err)
	h.start(t)
	h.await(t, second, "awaiting_authorization")
	awaitRecoveryDeliverySettled(t, h, second)
	h.stop(t)
	denied, err := h.store.GetTask(t.Context(), second)
	require.NoError(t, err)
	var original, parked map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(retained.Snapshot, &original))
	require.NoError(t, json.Unmarshal(denied.Snapshot, &parked))
	require.JSONEq(t, string(original["recovery"]), string(parked["recovery"]))
	p.mu.Lock()
	require.Equal(t, beforeDenied, messageCalls, "revoked identity must not call the inbox")
	p.mu.Unlock()
	require.NoError(t, h.store.RevokeUser(t.Context(), retained.UserID, false))
	_, err = h.store.Pool().Exec(context.Background(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(
	 snapshot,'{recovery,retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		second, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339))
	require.NoError(t, err)
	p.mu.Lock()
	blocked = false
	before := messageCalls
	p.mu.Unlock()
	// Force two parked attempts. Queue execution, not a version increment,
	// proves the attempt ran; neither may call the inbox without newer input.
	for range 2 {
		retryParkedRecoveryOnce(t, h, second)
		p.mu.Lock()
		calls := messageCalls
		p.mu.Unlock()
		require.Equal(t, before, calls)
	}
	h.start(t)
	third := submitFile(t, h, 3, time.Now())
	h.stop(t)
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
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
	for _, reason := range []string{"context_unavailable", "model_not_allowed"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			action := "resolve_blocker_then_resume"
			if reason == "model_not_allowed" {
				action = "select_allowed_model_or_restore_access"
			}
			testRecoveryReplacement(t, reason, action)
		})
	}
}

func testRecoveryReplacement(t *testing.T, reason, action string) {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	var predecessor string
	var replacementTarget, rejectedText string
	var staleLocator bool
	busyCreations := 0
	var savedBody string
	attempts := 0
	var delivered loom.RecoveryReceipt
	p.loom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			if recoveryUpgradePending(w, p, &staleLocator) {
				return
			}
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
			if predecessor == "" {
				respond(w, loom.ActiveRun{Status: "idle", NextAction: "create_run"})
				return
			}
			respond(
				w,
				loom.ActiveRun{
					RunID:      replacementTarget,
					Status:     "blocked",
					Reason:     reason,
					NextAction: action,
				},
			)
		case "/v1/chats/retained-chat/messages":
			p.mu.Lock()
			defer p.mu.Unlock()
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			attempts++
			if attempts == 1 {
				var request loom.RecoverMessage
				assert.NoError(t, json.Unmarshal(body, &request))
				assert.Equal(t, predecessor, request.ReplaceRunID)
				rejectedText = request.Text
				replacementTarget = "racing-predecessor"
				p.runs[replacementTarget] = loom.Run{ID: replacementTarget, Status: "running",
					ChatID: "retained-chat", AuthSessionID: "session"}
				w.WriteHeader(http.StatusConflict)
				respond(w, map[string]any{"error": map[string]string{"code": "replacement_target_changed"}})
				return
			}
			if attempts == 2 {
				savedBody = string(body)
				var request loom.RecoverMessage
				assert.NoError(t, json.Unmarshal(body, &request))
				assert.Equal(t, replacementTarget, request.ReplaceRunID)
				assert.Equal(t, rejectedText, request.Text)
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
			if predecessor == "" {
				p.mu.Unlock()
				p.agent(w, r)
				return
			}
			busyCreations++
			p.mu.Unlock()
			w.WriteHeader(http.StatusConflict)
			respond(w, map[string]any{"error": map[string]string{"code": "chat_busy"}})
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
	replacementTarget = predecessor
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
	require.GreaterOrEqual(t, attempts, 3)
	require.Equal(t, 1, busyCreations, "an older capability response must recover the typed chat_busy rejection")
	require.Len(t, p.createdInputs, 1)
}

func recoveryUpgradePending(w http.ResponseWriter, p *peers, pending *bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !*pending {
		return false
	}
	*pending = false
	respond(w, map[string]any{"output_validation": map[string]any{
		"version": 1, "disallow_custom_emoji": true}})
	return true
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
