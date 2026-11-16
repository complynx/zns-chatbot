package task

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestRecoveryReplaceTargetKeepsTerminalWorkInANewRound(t *testing.T) {
	t.Parallel()
	require.Empty(t, recoveryReplaceTarget(loom.ActiveRun{
		RunID: "finished", Status: "terminal", NextAction: actionSelectModel,
	}))
	require.Empty(t, recoveryReplaceTarget(loom.ActiveRun{
		RunID: "usable", Status: "accepted", Reason: "step_limit", NextAction: "ask_user_to_continue",
	}))
	require.Equal(t, "unusable", recoveryReplaceTarget(loom.ActiveRun{
		RunID: "unusable", Status: "blocked", Reason: "context_unavailable", NextAction: "resolve_blocker_then_resume",
	}))
}

func TestRecoveryOrderKeepsInteractiveLatencyThenSlowsParkedQueues(t *testing.T) {
	t.Parallel()
	s := &Service{options: Options{PollInterval: 20 * time.Millisecond}}
	var recent, parked *river.JobSnoozeError
	require.ErrorAs(t, s.waitForRecoveryOrder(db.Task{CreatedAt: time.Now()}), &recent)
	require.Equal(t, s.options.PollInterval, recent.Duration)
	require.ErrorAs(t, s.waitForRecoveryOrder(db.Task{CreatedAt: time.Now().Add(-time.Hour)}), &parked)
	require.Equal(t, time.Minute, parked.Duration)
}

func TestRecoveryTransientBackoffSurvivesRestartAndReopensWithNewInput(t *testing.T) {
	t.Parallel()
	s := &Service{options: Options{PollInterval: time.Second}}
	c := checkpoint{Recovery: &messageRecovery{RetryUntil: time.Now().Add(recoveryRetryWindow)}}
	initial := s.recoveryRetryDelay(c)
	c.Recovery.RetryUntil = time.Now().Add(recoveryRetryWindow - time.Minute)
	later := s.recoveryRetryDelay(c)
	require.Greater(t, later, initial)
	require.LessOrEqual(t, later, 10*time.Second)
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	var restored checkpoint
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.Equal(t, later, s.recoveryRetryDelay(restored))
	restored.Recovery.RetryUntil = time.Now().Add(recoveryRetryWindow)
	require.Equal(t, initial, s.recoveryRetryDelay(restored))
}

func TestSessionVersionConflictAdoptsDurableReceiptWithoutDenyingObserver(t *testing.T) {
	t.Parallel()
	for _, lifetime := range []time.Duration{time.Hour, sessionRenewSkew / 2} {
		t.Run(lifetime.String(), func(t *testing.T) {
			t.Parallel()
			testSessionVersionConflictReceipt(t, lifetime)
		})
	}
}

func testSessionVersionConflictReceipt(t *testing.T, lifetime time.Duration) {
	t.Helper()
	store := menuScanStore(t)
	observer, _ := availabilityIntake(t, store, 1)
	observer, err := store.Route(t.Context(), observer.ID)
	require.NoError(t, err)
	observer, err = store.Claim(t.Context(), observer.ID, "session-observer", time.Minute)
	require.NoError(t, err)
	observer.RunID, observer.AuthSessionID, observer.State = "live-run", "retained-session", "auth_reauthorizing"
	old := checkpoint{Session: loom.Session{ID: observer.AuthSessionID, Version: 1}, Reauthorizing: true,
		Run: &loom.Run{Status: runWaitingAuth, Reason: reasonAuthorizationRequired}}
	observer.Snapshot, err = json.Marshal(old)
	require.NoError(t, err)
	observer, err = store.Save(t.Context(), observer, "session-observer", observer.Fence, nil)
	require.NoError(t, err)
	receipt, _ := availabilityIntake(t, store, 2)
	newer := checkpoint{
		Session: loom.Session{ID: observer.AuthSessionID, Version: 2, ExpiresAt: time.Now().Add(lifetime)},
	}
	raw, err := json.Marshal(newer)
	require.NoError(t, err)
	_, err = store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET state='completed',auth_session_id=$2,
	 snapshot=$3 WHERE id=$1`, receipt.ID, observer.AuthSessionID, raw)
	require.NoError(t, err)
	s := &Service{store: store, owner: "session-observer", options: Options{PollInterval: time.Second}}
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, s.sessionError(t.Context(), &observer, &old, intent{}, &loom.Error{
		StatusCode: http.StatusConflict, Outcome: loom.Rejected, Code: "session_version_conflict",
	}), &snooze)
	current, err := store.GetTask(t.Context(), observer.ID)
	require.NoError(t, err)
	require.Equal(t, stateAwaitingService, current.State)
	require.Equal(t, "live-run", current.RunID)
	require.EqualValues(t, 2, old.Session.Version)
	require.False(t, old.Reauthorizing)
	require.False(t, old.SessionVersionUnknown)
	require.Nil(t, old.Run)
	rows, err := store.ListOutbox(t.Context(), observer.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestRecoveryOrdinaryHandoffWaitsWithoutSendingAdministratorNotice(t *testing.T) {
	t.Parallel()
	s := &Service{options: Options{PollInterval: 20 * time.Millisecond}}
	var snooze *river.JobSnoozeError
	// No store or delivery dependencies: an ordinary pending final save must
	// only reschedule observation, without producing any operator notice.
	err := s.recoveryAttachmentError(
		t.Context(), &db.Task{CreatedAt: time.Now()}, checkpoint{}, intent{}, db.ErrRoutingPending,
	)
	require.ErrorAs(t, err, &snooze)
	require.Equal(t, s.options.PollInterval, snooze.Duration)
}

func TestInboxReconciliationLeavesTelegramReceiptUncertaintySeparate(t *testing.T) {
	t.Parallel()
	r := &messageRecovery{ChatID: "chat", Request: loom.RecoverMessage{Text: "original"}}
	require.True(t, canReconcileInbox(stateDeliveryUnknown, r))
	r.Rejected = true
	require.False(t, canReconcileInbox(stateDeliveryUnknown, r))
	r.Rejected = false
	r.Receipt = &loom.RecoveryReceipt{Accepted: true, Delivered: true, RunID: "run"}
	require.False(t, canReconcileInbox(stateDeliveryUnknown, r))
	require.True(t, canReconcileInbox(stateAuthUnknown, r), "unknown renewal can resume delivered observation")
	r.Receipt.Abandoned = true
	require.False(t, canReconcileInbox(stateAuthUnknown, r))
}

func TestPendingWithdrawalPreventsWorkerReplayWithoutAReceipt(t *testing.T) {
	t.Parallel()
	s := &Service{}
	c := checkpoint{Recovery: &messageRecovery{Withdrawal: &recoveryWithdrawal{OperationID: "withdraw"}}}
	var snooze *river.JobSnoozeError
	// No IAM, database or Loom dependency may be used while an operator owns withdrawal.
	require.ErrorAs(t, s.recoverConversation(t.Context(), &db.Task{}, c, intent{}, db.User{}, ""), &snooze)
	require.Equal(t, time.Minute, snooze.Duration)
}

func TestRecoveryPauseActionsKeepWalletAndContextDistinct(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ status, reason, action string }{
		{runPaused, "credits_exhausted", "fund_wallet_then_retry"},
		{runPaused, "recovery_context_unavailable", "stop_run_then_send_message"},
		{runPaused, "recovery_context_too_large", "stop_run_then_send_message"},
		{runPaused, "client_pause", ""},
		{"running", "credits_exhausted", ""},
	} {
		require.Equal(t, tc.action, recoveryPauseAction(loom.Run{Status: tc.status, Reason: tc.reason}))
	}
}
