package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

type recoveryResolutionPeer struct {
	t                       *testing.T
	p                       *peers
	runID                   string
	abandoned               bool
	loseReceipt             bool
	invalid                 bool
	denyAccess              bool
	delivered               string
	predecessorReadFailures int
	bodies, keys            []string
	receiptAction           string
	withdrawalKeys          []string
	loseWithdrawal          bool
	withdrawalDelivered     bool
}

func (f *recoveryResolutionPeer) serve(w http.ResponseWriter, r *http.Request) {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	if f.delivered != "" && r.URL.Path == "/v1/runs/"+f.runID && f.predecessorReadFailures > 0 {
		f.predecessorReadFailures--
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	located := loom.ActiveRun{Status: "idle", NextAction: "create_run"}
	if f.runID != "" {
		located = loom.ActiveRun{RunID: f.runID, Status: "blocked", Reason: "authorization_required",
			NextAction: "reauthorize_session"}
	}
	if f.delivered != "" {
		located = loom.ActiveRun{RunID: f.delivered, Status: "accepted", NextAction: "send_message"}
	}
	if respondRecoveryDiscovery(w, r, located) {
		return
	}
	switch r.URL.Path {
	case "/v1/chats/retained-chat/messages":
		body, err := io.ReadAll(r.Body)
		assert.NoError(f.t, err)
		f.bodies = append(f.bodies, string(body))
		f.keys = append(f.keys, r.Header.Get("Idempotency-Key"))
		if f.denyAccess {
			w.WriteHeader(http.StatusForbidden)
			respond(w, map[string]any{"error": map[string]string{"code": "forbidden"}})
			return
		}
		if f.invalid {
			w.WriteHeader(http.StatusUnprocessableEntity)
			respond(w, map[string]any{"error": map[string]string{"code": "invalid_message"}})
			return
		}
		if f.abandoned && f.loseReceipt {
			f.loseReceipt = false
			disconnect(w)
			return
		}
		respond(w, f.receipt(r.Header.Get("Idempotency-Key")))
	default:
		if strings.HasPrefix(r.URL.Path, "/v1/chats/retained-chat/messages/") &&
			strings.HasSuffix(r.URL.Path, "/abandon") {
			f.withdraw(w, r)
			return
		}
		f.p.mu.Unlock()
		f.p.agent(w, r)
		f.p.mu.Lock()
	}
}

func (f *recoveryResolutionPeer) receipt(key string) loom.RecoveryReceipt {
	receipt := loom.RecoveryReceipt{Accepted: true, MessageID: key, Sequence: 1,
		Status: "blocked", Reason: "authorization_required", NextAction: "reauthorize_session"}
	if f.receiptAction != "" {
		receipt.Reason, receipt.NextAction = "replacement_target_changed", f.receiptAction
	}
	if f.abandoned {
		receipt.Abandoned, receipt.Status, receipt.Reason = true, "abandoned", "explicit_withdrawal"
		receipt.NextAction = "continue_or_create_new_request"
	}
	if f.delivered != "" {
		receipt.Delivered, receipt.RunID, receipt.InputID = true, f.delivered, "reconciled-input"
		receipt.Status, receipt.Reason, receipt.NextAction = "accepted", "", "read_run"
	}
	return receipt
}

func (f *recoveryResolutionPeer) withdraw(w http.ResponseWriter, r *http.Request) {
	f.withdrawalKeys = append(f.withdrawalKeys, r.Header.Get("Idempotency-Key"))
	if f.withdrawalDelivered {
		w.WriteHeader(http.StatusConflict)
		respond(w, map[string]any{"error": map[string]string{"code": "message_already_delivered"}})
		return
	}
	f.abandoned = true
	if f.loseWithdrawal {
		f.loseWithdrawal = false
		disconnect(w)
		return
	}
	message := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/chats/retained-chat/messages/"), "/abandon")
	respond(w, loom.RecoveryReceipt{Accepted: true, Abandoned: true, MessageID: message, Sequence: 1,
		Status: "abandoned", Reason: "explicit_withdrawal", NextAction: "continue_or_create_new_request"})
}

func retainedRecoveryFixture(t *testing.T, action string) (*harness, *recoveryResolutionPeer, uuid.UUID) {
	t.Helper()
	p := newPeers(t)
	f := &recoveryResolutionPeer{p: p, t: t, runID: "retained-run", receiptAction: action}
	p.runs[f.runID] = loom.Run{ID: f.runID, ChatID: "retained-chat", Status: "waiting_auth",
		Reason: "authorization_required", Version: 1, AuthSessionID: "retained-session"}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	id := h.submitText(t, 1241, "Keep this exact retained request")
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(t.Context(), id)
		return err == nil && strings.Contains(string(current.Snapshot), `"message_id":`)
	}, 10*time.Second, 20*time.Millisecond)
	awaitRecoveryDeliverySettled(t, h, id)
	h.stop(t)
	return h, f, id
}

func TestRecoveryOperatorWithdrawalRetainsIntentAcrossLostReceipt(t *testing.T) {
	t.Parallel()
	h, f, id := retainedRecoveryFixture(t, "abandon_message_or_inspect_run")
	operator := taskAdminToken(t, h, id)
	before := adminDiagnostics(t, h, operator, id)
	path := "/internal/admin/tasks/" + id.String() + "/abandon-recovery"
	operation := strings.Repeat("w", 128)
	body := map[string]any{"operation_id": operation, "expected_version": before.Task.Version,
		"reason": "Withdraw obsolete replacement target", "confirm": false}
	code, _ := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusBadRequest, code)
	body["confirm"] = true
	require.NoError(t, h.store.RevokeUser(t.Context(), before.Task.UserID, true))
	code, _ = adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusForbidden, code)
	require.NoError(t, h.store.RevokeUser(t.Context(), before.Task.UserID, false))
	f.p.mu.Lock()
	require.Empty(t, f.withdrawalKeys)
	f.loseWithdrawal = true
	f.p.mu.Unlock()
	code, _ = adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusServiceUnavailable, code)
	unknown, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.NotEqual(t, "closed", unknown.State)
	require.Contains(t, string(unknown.Snapshot), `"withdrawal":`)
	require.NotContains(t, string(unknown.Snapshot), `"abandoned": true`)
	h.start(t)
	h.stop(t)
	body["reason"] = "Changed operation must not reuse the saved intent"
	code, _ = adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusConflict, code)
	body["reason"] = "Withdraw obsolete replacement target"
	code, result := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(result))
	code, replay := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(replay))
	var resolved, repeated db.TaskResolution
	require.NoError(t, json.Unmarshal(result, &resolved))
	require.NoError(t, json.Unmarshal(replay, &repeated))
	require.Equal(t, resolved, repeated)
	after := adminDiagnostics(t, h, operator, id)
	require.Equal(t, "closed", after.Task.State)
	require.Greater(t, after.Task.Fence, before.Task.Fence)
	require.Len(t, after.Resolutions, 1)
	require.Equal(t, db.RecoveryAbandonmentPrefix+operation, after.Resolutions[0].OperationID)
	f.p.mu.Lock()
	require.Len(t, f.withdrawalKeys, 2)
	require.LessOrEqual(t, len(f.withdrawalKeys[0]), 128)
	require.Equal(t, f.withdrawalKeys[0], f.withdrawalKeys[1])
	require.Len(t, f.bodies, 1, "worker cannot replay a message with an unknown withdrawal")
	f.runID = ""
	f.p.mu.Unlock()
	h.start(t)
	later := h.submitText(t, 1242, "A later request can proceed after explicit withdrawal")
	h.await(t, later, "completed")
}

func TestRecoveryWithdrawalCannotCloseAnAlreadyDeliveredMessage(t *testing.T) {
	t.Parallel()
	h, f, id := retainedRecoveryFixture(t, "abandon_message_or_inspect_run")
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,
	 '{recovery,retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	operator := taskAdminToken(t, h, id)
	before := adminDiagnostics(t, h, operator, id)
	f.p.mu.Lock()
	f.withdrawalDelivered = true
	f.p.mu.Unlock()
	code, result := adminRequest(t, h, operator, http.MethodPost,
		"/internal/admin/tasks/"+id.String()+"/abandon-recovery", map[string]any{
			"operation_id": "already-delivered", "expected_version": before.Task.Version,
			"reason": "Withdraw only if still undelivered", "confirm": true,
		})
	require.Equal(t, http.StatusConflict, code, string(result))
	require.Contains(t, string(result), "message_already_delivered")
	after, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.NotEqual(t, "closed", after.State)
	require.NotContains(t, string(after.Snapshot), `"withdrawal":`)
	require.Empty(t, adminDiagnostics(t, h, operator, id).Resolutions)
	f.p.mu.Lock()
	f.delivered = f.runID
	run := f.p.runs[f.runID]
	run.Status, run.Reason, run.Result = "completed", "", "Already delivered result"
	f.p.runs[f.runID] = run
	f.p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE river_job SET scheduled_at=now() WHERE state='scheduled'")
	require.NoError(t, err)
	h.start(t)
	h.await(t, id, "completed")
	h.awaitReplies(t, 2)
	awaitRecoveryDeliverySettled(t, h, id)
	h.stop(t)
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Len(t, f.bodies, 2, "positive delivery evidence must reopen receipt replay without new customer input")
	require.Equal(t, f.bodies[0], f.bodies[1])
	require.Equal(t, f.keys[0], f.keys[1])
	require.Empty(t, f.p.createdInputs)
	require.Equal(t, "Already delivered result", f.p.sent[len(f.p.sent)-1].Text)
}

func TestRecoveryStaticPauseUsesMinuteQueueCadence(t *testing.T) {
	t.Parallel()
	h, f, id := retainedRecoveryFixture(t, "resolve_blocker_then_resume")
	require.Eventually(t, func() bool {
		var parked bool
		err := h.store.Pool().QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM river_job
		 WHERE kind='shuttle_task' AND args->>'task_id'=$1 AND state='scheduled'
		 AND scheduled_at>now()+interval '40 seconds')`, id.String()).Scan(&parked)
		return err == nil && parked
	}, 5*time.Second, 20*time.Millisecond)
	before, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	retryParkedRecoveryOnce(t, h, id)
	after, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, before.Version, after.Version, "unchanged parks must not invalidate an operator's version")
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,
	 '{recovery,retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	retryParkedRecoveryOnce(t, h, id)
	_, err = h.store.Pool().Exec(t.Context(),
		"UPDATE shuttle_conversations SET active_run_id='stale-pointer' WHERE id=$1", after.ConversationID)
	require.NoError(t, err)
	retryParkedRecoveryOnce(t, h, id)
	var restored string
	require.NoError(t, h.store.Pool().QueryRow(t.Context(),
		"SELECT active_run_id FROM shuttle_conversations WHERE id=$1", after.ConversationID).Scan(&restored))
	require.Equal(t, after.RunID, restored, "skipping a checkpoint must not skip conversation pointer repair")
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Len(t, f.bodies, 2, "expired static blocker must not replay without newer customer input")
	for _, sent := range f.p.sent {
		require.NotContains(t, sent.Text, "Send another message to retry")
	}
}

func TestRecoveryNewCustomerInputRepeatsAnUnresolvedBlocker(t *testing.T) {
	t.Parallel()
	h, f, id := retainedRecoveryFixture(t, "fund_wallet_then_retry")
	h.start(t)
	h.submitText(t, 1242, "I added funds, please continue")
	h.stop(t)
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,
	 '{recovery,retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	retryParkedRecoveryOnce(t, h, id)
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	notices := 0
	for _, sent := range f.p.sent {
		if strings.Contains(sent.Text, "Add funds") {
			notices++
		}
	}
	require.Equal(t, 2, notices, "new customer input needs a current blocker status")
	require.Len(t, f.bodies, 2)
	require.Equal(t, f.bodies[0], f.bodies[1])
	require.Equal(t, f.keys[0], f.keys[1])
	require.Empty(t, f.p.createdInputs)
}

func retryParkedRecoveryOnce(t *testing.T, h *harness, id uuid.UUID) {
	t.Helper()
	var boundary time.Time
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&boundary))
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE river_job SET scheduled_at=now()
	 WHERE kind='shuttle_task' AND args->>'task_id'=$1 AND state='scheduled'`, id.String())
	require.NoError(t, err)
	h.start(t)
	require.Eventually(t, func() bool {
		var parked bool
		err = h.store.Pool().QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM river_job WHERE kind='shuttle_task'
		 AND args->>'task_id'=$1 AND attempted_at>$2 AND state='scheduled'
		 AND scheduled_at>now()+interval '40 seconds')`, id.String(), boundary).Scan(&parked)
		return err == nil && parked
	}, 5*time.Second, 20*time.Millisecond)
	h.stop(t)
}

func TestRecoveryReconciledDeliveryObservesSuccessorAcrossRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.loseReauthorization = true, true
	f := &recoveryResolutionPeer{p: p, t: t}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 1221)
	awaitLoomRun(t, h, first)
	h.stop(t)
	prior, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	f.runID = prior.RunID
	run := p.runs[first.String()]
	run.Status, run.Reason, run.ChatID = "waiting_auth", "authorization_required", "retained-chat"
	p.runs[first.String()] = run
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET state='failed' WHERE id=$1", first)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_conversations SET active_task_id=NULL,
	 active_run_id='' WHERE id=$1`, prior.ConversationID)
	require.NoError(t, err)
	h.start(t)
	id := h.submitText(t, 1222, "Keep observing this accepted message")
	h.await(t, id, "auth_unknown")
	awaitRecoveryDeliverySettled(t, h, id)
	h.stop(t)
	p.mu.Lock()
	run.Status, run.Reason = "cancelled", "superseded"
	p.runs[first.String()] = run
	f.delivered = "reconciled-successor"
	f.predecessorReadFailures = 1
	p.runs["successor"] = loom.Run{ID: f.delivered, ReplacesRunID: run.ID, AuthSessionID: run.AuthSessionID,
		ChatID: run.ChatID, Status: "running", Version: 1}
	p.mu.Unlock()
	operator := taskAdminToken(t, h, id)
	before := adminDiagnostics(t, h, operator, id)
	path := "/internal/admin/tasks/" + id.String() + "/reconcile-recovery"
	body := map[string]any{"operation_id": "observe-delivered", "expected_version": before.Task.Version,
		"reason": "Observe the authoritative delivered successor", "confirm": true}
	code, result := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(result))
	code, replay := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(replay))
	var resolved, repeated db.TaskResolution
	require.NoError(t, json.Unmarshal(result, &resolved))
	require.NoError(t, json.Unmarshal(replay, &repeated))
	require.Equal(t, resolved, repeated)
	h.start(t)
	require.Eventually(t, func() bool {
		current, readErr := h.store.GetTask(t.Context(), id)
		return readErr == nil && current.RunID == "reconciled-successor" && current.State == "running"
	}, 10*time.Second, 20*time.Millisecond)
	awaitRecoveryDeliverySettled(t, h, id)
	later := h.submitText(t, 1223, "Add this after the unknown renewal was reconciled")
	h.await(t, later, "completed")
	h.stop(t)
	p.mu.Lock()
	run = p.runs["successor"]
	run.Status, run.Result = "completed", "Reconciled successor result"
	p.runs["successor"] = run
	p.mu.Unlock()
	h.start(t)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		current, readErr := h.store.GetTask(t.Context(), id)
		assert.NoError(collect, readErr)
		var metadata map[string]json.RawMessage
		_ = json.Unmarshal(current.Snapshot, &metadata)
		assert.Equal(collect, "completed", current.State, "run=%s reauthorizing=%s unknown=%s discovery=%s read=%s",
			metadata["run"], metadata["reauthorizing"], metadata["session_version_unknown"],
			metadata["discovery_next_check"], metadata["run_read_next_check"])
	}, 10*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		rows, readErr := h.store.ListOutbox(t.Context(), id)
		for _, row := range rows {
			if readErr == nil && row.State == "delivered" && strings.Contains(string(row.Payload), run.Result) {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	current, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.Contains(t, string(current.Snapshot), `"session_version_unknown": true`)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Zero(t, f.predecessorReadFailures)
	require.Len(t, f.bodies, 3)
	require.Equal(t, f.bodies[0], f.bodies[1])
	require.Equal(t, []string{id.String(), id.String(), later.String()}, f.keys)
	require.Len(t, p.createdInputs, 1)
	count := 0
	for _, sent := range p.sent {
		require.NotContains(t, sent.Text, "agent service is unavailable")
		if strings.Contains(sent.Text, run.Result) {
			count++
		}
	}
	require.Equal(t, 1, count)
}

// These restarts test inbox observation, so finish existing Telegram sends first.
// A stopped send has an intentionally unknown receipt and cannot be replayed.
func awaitRecoveryDeliverySettled(t *testing.T, h *harness, id uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(t.Context(), id)
		if err != nil || current.LeaseOwner != "" {
			return false
		}
		rows, err := h.store.ListOutbox(t.Context(), id)
		if err != nil || len(rows) == 0 {
			return false
		}
		for _, row := range rows {
			if row.State != "delivered" {
				return false
			}
		}
		return true
	}, 10*time.Second, 20*time.Millisecond)
}

func TestRecoveryDefinitiveRejectionCanBeClosedAfterLostNotification(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.loseSend = true, true
	f := &recoveryResolutionPeer{p: p, t: t, invalid: true}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 1211)
	awaitLoomRun(t, h, first)
	prior, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	f.runID = prior.RunID
	p.mu.Unlock()
	id := h.submitText(t, 1212, "Rejected before inbox acceptance")
	h.await(t, id, "delivery_unknown")
	h.stop(t)
	operator := taskAdminToken(t, h, id)
	before := adminDiagnostics(t, h, operator, id)
	code, result := adminRequest(t, h, operator, http.MethodPost, "/internal/admin/tasks/"+id.String()+"/close",
		map[string]any{"operation_id": "close-rejected", "expected_version": before.Task.Version,
			"reason": "Stop notification after definitive inbox rejection", "confirm": true})
	require.Equal(t, http.StatusOK, code, string(result))
	require.Equal(t, "closed", adminDiagnostics(t, h, operator, id).Task.State)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, f.bodies, 1)
	require.Len(t, p.createdInputs, 1)
	require.Zero(t, p.sessionReauthorizations)
}

func TestRecoveryOperatorReconcilesExternalAbandonmentWithoutRenewingUnknownAuthorization(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.loseReauthorization, p.loseSend = true, true, true
	f := &recoveryResolutionPeer{p: p, t: t}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 1201)
	awaitLoomRun(t, h, first)
	h.stop(t)
	prior, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	f.runID = prior.RunID
	run := p.runs[first.String()]
	run.Status, run.Reason, run.ChatID = "waiting_auth", "authorization_required", "retained-chat"
	p.runs[first.String()] = run
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), "UPDATE shuttle_tasks SET state='failed' WHERE id=$1", first)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_conversations SET active_task_id=NULL,
	 active_run_id='' WHERE id=$1`, prior.ConversationID)
	require.NoError(t, err)
	h.start(t)
	id := h.submitText(t, 1202, "Retain this original message")
	h.await(t, id, "auth_unknown")
	require.Eventually(t, func() bool {
		rows, readErr := h.store.ListOutbox(t.Context(), id)
		return readErr == nil && len(rows) > 0 && rows[0].State == "delivery_unknown"
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	h.start(t)
	h.stop(t)
	operator := taskAdminToken(t, h, id)
	rows, err := h.store.ListOutbox(t.Context(), id)
	require.NoError(t, err)
	states := make([]string, len(rows))
	for n := range rows {
		states[n] = rows[n].State
	}
	require.Contains(t, states, "delivery_unknown")
	before := adminDiagnostics(t, h, operator, id)
	body := map[string]any{"operation_id": "reconcile-retained", "expected_version": before.Task.Version,
		"reason": "Refresh receipt after explicit external withdrawal", "confirm": false}
	path := "/internal/admin/tasks/" + id.String() + "/reconcile-recovery"
	code, _ := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusBadRequest, code)
	body["confirm"] = true
	p.mu.Lock()
	f.denyAccess = true
	p.mu.Unlock()
	code, rejected := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusForbidden, code)
	require.Contains(t, string(rejected), "forbidden")
	require.Equal(t, before.Task.Version, adminDiagnostics(t, h, operator, id).Task.Version)
	p.mu.Lock()
	f.denyAccess = false
	f.abandoned, f.loseReceipt = true, true
	p.mu.Unlock()
	body["confirm"] = true
	code, _ = adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusServiceUnavailable, code)
	code, result := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(result))
	code, replayed := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(replayed))
	var resolution, repeated db.TaskResolution
	require.NoError(t, json.Unmarshal(result, &resolution))
	require.NoError(t, json.Unmarshal(replayed, &repeated))
	require.Equal(t, resolution, repeated)
	current, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	var saved struct {
		Reauthorizing bool `json:"reauthorizing"`
		Recovery      struct {
			Receipt loom.RecoveryReceipt `json:"receipt"`
		} `json:"recovery"`
	}
	require.NoError(t, json.Unmarshal(current.Snapshot, &saved))
	require.True(t, saved.Reauthorizing)
	require.True(t, saved.Recovery.Receipt.Abandoned)
	require.Equal(t, "auth_unknown", current.State)
	body["operation_id"] = db.RecoveryReconciliationPrefix + "reconcile-retained"
	code, _ = adminRequest(t, h, operator, http.MethodPost, "/internal/admin/tasks/"+id.String()+"/close", body)
	require.Equal(t, http.StatusBadRequest, code, "reconciliation audit cannot be replayed as closure")
	body["operation_id"], body["expected_version"] = "close-reconciled", current.Version
	code, result = adminRequest(t, h, operator, http.MethodPost, "/internal/admin/tasks/"+id.String()+"/close", body)
	require.Equal(t, http.StatusOK, code, string(result))
	require.Equal(t, "closed", adminDiagnostics(t, h, operator, id).Task.State)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Len(t, p.createdInputs, 1)
	require.Len(t, f.bodies, 4)
	for n := range f.bodies {
		require.Equal(t, f.bodies[0], f.bodies[n])
		require.Equal(t, id.String(), f.keys[n])
	}
}
