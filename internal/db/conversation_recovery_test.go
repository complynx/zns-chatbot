package db_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestRecoveryWakeSequenceExcludesInternalStartContinuations(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	pending := accepted(t, s, 1)
	child := accepted(t, s, 2)
	_, err := s.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET request=request ||
	 '{"start_continuation":"original-request"}'::jsonb WHERE id=$1`, child.ID)
	require.NoError(t, err)
	_, seq, err := s.RecoveryReady(t.Context(), pending)
	require.NoError(t, err)
	require.Equal(t, pending.Seq, seq)
	next := accepted(t, s, 3)
	_, seq, err = s.RecoveryReady(t.Context(), pending)
	require.NoError(t, err)
	require.Equal(t, next.Seq, seq)
}

func TestAdminClosureReleasesParkedObserver(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	parent, queued := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	parent.State = "awaiting_service"
	parent.Snapshot = json.RawMessage(`{"recovery":{"receipt":{"delivered":true}}}`)
	parent, err := s.Save(t.Context(), parent, parent.LeaseOwner, parent.Fence, nil)
	require.NoError(t, err)
	require.NoError(t, s.Release(t.Context(), parent.ID, parent.LeaseOwner, parent.Fence))
	u, err := s.GetUser(t.Context(), parent.UserID)
	require.NoError(t, err)
	request := db.CloseTaskRequest{TaskID: parent.ID, BotID: u.BotID, Organization: u.Organization,
		OperationID: "close-parked", Actor: "operator", Reason: "Upstream state reconciled", ExpectedVersion: parent.Version}
	closed, err := s.CloseUncertainTask(t.Context(), request)
	require.NoError(t, err)
	replayed, err := s.CloseUncertainTask(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, closed, replayed)
	_, err = s.Save(t.Context(), parent, parent.LeaseOwner, parent.Fence, nil)
	require.ErrorIs(t, err, db.ErrLeaseLost)
	queued, err = s.Claim(t.Context(), queued.ID, "next-worker", time.Minute)
	require.NoError(t, err)
	queued, err = s.PromoteMessage(t.Context(), queued.ID, "next-worker", queued.Fence)
	require.NoError(t, err)
	require.Equal(t, "new_run", queued.Route)
}

func TestRecoveryObserverTransferFencesLateParentSave(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	parent, input := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	input, err := s.Claim(ctx, input.ID, "recovery-worker", time.Minute)
	require.NoError(t, err)
	ready, _, err := s.RecoveryReady(ctx, input)
	require.NoError(t, err)
	require.True(t, ready)
	input.RunID = "successor"
	input.AuthSessionID = parent.AuthSessionID
	updated, owns, err := s.OwnRecoveredRun(
		ctx,
		input,
		"recovery-worker",
		json.RawMessage(`{"run":{"replaces_run_id":"predecessor"},
		 "recovery":{"superseded_run_id":"predecessor","receipt":{"delivered":true}}}`),
	)
	require.NoError(t, err)
	require.True(t, owns)
	require.Equal(t, "new_run", updated.Route)
	old, err := s.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, "closed", old.State)
	require.Greater(t, old.Fence, parent.Fence)
	_, err = s.Save(ctx, parent, parent.LeaseOwner, parent.Fence, nil)
	require.ErrorIs(t, err, db.ErrLeaseLost)
	next := accepted(t, s, 3)
	next, err = s.Route(ctx, next.ID)
	require.NoError(t, err)
	require.Equal(t, updated.ID, next.TargetTaskID)
}

func TestRecoveryKeepsExistingObserverAndOrdersPendingInputs(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	parent, input := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	observer, err := s.RecoveryObserver(ctx, input)
	require.NoError(t, err)
	require.Equal(t, parent, observer)
	foreign := input
	foreign.UserID = uuid.New()
	observer, err = s.RecoveryObserver(ctx, foreign)
	require.NoError(t, err)
	require.Equal(t, db.Task{}, observer, "an observer lookup must stay within its owner")
	input, err = s.Claim(ctx, input.ID, "recovery-worker", time.Minute)
	require.NoError(t, err)
	input.RunID = parent.RunID
	parent.State = "auth_unknown"
	parent, err = s.Save(ctx, parent, "parent-worker", parent.Fence, nil)
	require.NoError(t, err)
	_, _, err = s.OwnRecoveredRun(ctx, input, "recovery-worker", json.RawMessage(`{}`))
	require.ErrorIs(t, err, db.ErrRoutingPending)
	parent.State = "running"
	parent, err = s.Save(ctx, parent, "parent-worker", parent.Fence, nil)
	require.NoError(t, err)
	_, owns, err := s.OwnRecoveredRun(ctx, input, "recovery-worker", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.False(t, owns)
	next := accepted(t, s, 3)
	next, err = s.Route(ctx, next.ID)
	require.NoError(t, err)
	ready, seq, err := s.RecoveryReady(ctx, next)
	require.NoError(t, err)
	require.False(t, ready)
	require.Equal(t, next.Seq, seq)
	input.State = "awaiting_service"
	input.Snapshot = json.RawMessage(`{"recovery":{"receipt":{"delivered":true}}}`)
	input, err = s.Save(ctx, input, "recovery-worker", input.Fence, nil)
	require.NoError(t, err)
	ready, _, err = s.RecoveryReady(ctx, next)
	require.NoError(t, err)
	require.False(t, ready, "a delivered receipt must attach before the next message can replace its run")
	input.State = "completed"
	input.AuthSessionID = parent.AuthSessionID
	input.Snapshot = json.RawMessage(`{"session":{"auth_session_id":"session","version":3}}`)
	_, err = s.Save(ctx, input, "recovery-worker", input.Fence, nil)
	require.NoError(t, err)
	snapshot, err := s.RecoveryRunSession(ctx, parent.UserID, "successor", parent.AuthSessionID)
	require.NoError(t, err)
	require.JSONEq(t, string(input.Snapshot), string(snapshot), "handoff must retain the newest session receipt")
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET auth_session_id='other-session',
	 snapshot='{"session":{"auth_session_id":"other-session","version":99}}' WHERE id=$1`, next.ID)
	require.NoError(t, err)
	snapshot, err = s.RecoveryRunSession(ctx, parent.UserID, "", parent.AuthSessionID)
	require.NoError(t, err)
	require.JSONEq(t, string(input.Snapshot), string(snapshot), "an unanchored task must not shadow the exact session")
	ready, _, err = s.RecoveryReady(ctx, next)
	require.NoError(t, err)
	require.True(t, ready)
	old, err := s.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	require.NotEqual(t, "closed", old.State)
	// A lost input receipt can arrive after its original observer sent the result.
	parent.State = "completed"
	_, err = s.Save(ctx, parent, "parent-worker", parent.Fence, nil)
	require.NoError(t, err)
	next, err = s.Claim(ctx, next.ID, "late-receipt", time.Minute)
	require.NoError(t, err)
	next.RunID = parent.RunID
	_, owns, err = s.OwnRecoveredRun(ctx, next, "late-receipt", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.False(t, owns, "a late receipt must not redeliver the terminal run result")
	// A failed prompt delivery is not evidence that the run itself finished.
	parent.State = "delivery_failed"
	parent.Snapshot = json.RawMessage(`{"run":{"status":"waiting_approval"}}`)
	_, err = s.Save(ctx, parent, "parent-worker", parent.Fence, nil)
	require.NoError(t, err)
	_, owns, err = s.OwnRecoveredRun(ctx, next, "late-receipt", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.True(t, owns, "a still-live run needs a new observer after prompt delivery failure")
}

func runningRecoveryParent(t *testing.T, s *db.Store, parent db.Task) db.Task {
	t.Helper()
	ctx := context.Background()
	parent, err := s.Claim(ctx, parent.ID, "parent-worker", time.Minute)
	require.NoError(t, err)
	parent.RunID = "predecessor"
	parent.AuthSessionID = "session"
	parent.State = "running"
	parent, err = s.Save(ctx, parent, "parent-worker", parent.Fence, nil)
	require.NoError(t, err)
	return parent
}

func TestRecoverySteeringReactionUsesDurableInboxReceipt(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	row := accepted(t, s, 4)
	_, err := s.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET run_id='retained',
	 request='{"chat_id":2,"raw_update":{"message":{"message_id":51}}}',
	 snapshot='{"recovery":{"receipt":{"delivered":true,"input_id":"inbox-input"}}}' WHERE id=$1`, row.ID)
	require.NoError(t, err)
	chat, message, err := s.ReactionTarget(t.Context(), row.UserID, "input.steering", "retained", "inbox-input")
	require.NoError(t, err)
	require.EqualValues(t, 2, chat)
	require.EqualValues(t, 51, message)
}

func TestRecoveryNewRoundWaitsForPredecessorFinalSave(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	parent, input := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	input, err := s.Claim(ctx, input.ID, "new-round", time.Minute)
	require.NoError(t, err)
	input.RunID = "next-round"
	snapshot := json.RawMessage(`{"run":{"run_id":"next-round","replaces_run_id":"predecessor"}}`)
	_, _, err = s.OwnRecoveredRun(ctx, input, "new-round", snapshot)
	require.ErrorIs(t, err, db.ErrRoutingPending)
	require.NotErrorIs(t, err, db.ErrRecoveryAuthorizationUnknown)
	// The completed run's observer retains its lease and can commit its result.
	parent.State = "completed"
	_, err = s.Save(ctx, parent, "parent-worker", parent.Fence, []db.Outbound{{
		DedupKey: "previous-result", Payload: json.RawMessage(`{"text":"Previous final result"}`),
	}})
	require.NoError(t, err)
	_, owns, err := s.OwnRecoveredRun(ctx, input, "new-round", snapshot)
	require.NoError(t, err)
	require.True(t, owns)
	out, err := s.ListOutbox(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Contains(t, string(out[0].Payload), "Previous final result")
}

func TestRecoveryNewRoundFencesAlreadyObservedSupersession(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	parent, input := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	parent.State = "awaiting_service"
	parent.Snapshot = json.RawMessage(`{"run":{"run_id":"predecessor","status":"cancelled","reason":"superseded"}}`)
	parent, err := s.Save(t.Context(), parent, "parent-worker", parent.Fence, nil)
	require.NoError(t, err)
	input, err = s.Claim(t.Context(), input.ID, "later-round", time.Minute)
	require.NoError(t, err)
	input.RunID = "later-round"
	_, owns, err := s.OwnRecoveredRun(t.Context(), input, "later-round",
		json.RawMessage(`{"run":{"run_id":"later-round"},"recovery":{"receipt":{"delivered":true}}}`))
	require.NoError(t, err)
	require.True(t, owns)
	_, err = s.Save(t.Context(), parent, "parent-worker", parent.Fence, nil)
	require.ErrorIs(t, err, db.ErrLeaseLost)
}

func TestRecoveryCarriesDeferredStartSourcesAndKeepsOriginalChildKeys(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	parent, pending := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	_, err := s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET
	 request='{"start_continuation":"/profile"}',state='failed',
	 snapshot='{"welcome_after_onboarding":true,"run":{"status":"paused"}}' WHERE id=$1`, parent.ID)
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_tasks SET request='{"start_continuation":"another request"}',
	 state='completed',snapshot='{"recovery":{"receipt":{"delivered":true}}}' WHERE id=$1`, pending.ID)
	require.NoError(t, err)
	next := accepted(t, s, 3)
	next, err = s.Route(ctx, next.ID)
	require.NoError(t, err)
	next, err = s.Claim(ctx, next.ID, "handoff", time.Minute)
	require.NoError(t, err)
	next.RunID = "successor"
	next, owns, err := s.OwnRecoveredRun(ctx, next, "handoff",
		json.RawMessage(`{"run":{"replaces_run_id":"predecessor"}}`))
	require.NoError(t, err)
	require.True(t, owns)
	var saved struct {
		Origins []uuid.UUID `json:"recovery_start_origins"`
		Welcome bool        `json:"welcome_after_onboarding"`
	}
	require.NoError(t, json.Unmarshal(next.Snapshot, &saved))
	require.Equal(t, []uuid.UUID{parent.ID, pending.ID}, saved.Origins)
	require.True(t, saved.Welcome)
	payload := json.RawMessage(`{"kind":"message","text":"/profile"}`)
	_, err = s.IntakeRecoveredStartContinuation(ctx, parent.ID, next.ID, payload, nil)
	require.ErrorIs(t, err, db.ErrConflict, "delivery and completion must precede dispatch")
	next.State = "completed"
	next, err = s.Save(ctx, next, "handoff", next.Fence, nil)
	require.NoError(t, err)
	for _, source := range saved.Origins {
		child, intakeErr := s.IntakeRecoveredStartContinuation(ctx, source, next.ID, payload, nil)
		require.NoError(t, intakeErr)
		replayed, replayErr := s.IntakeRecoveredStartContinuation(ctx, source, next.ID, payload, nil)
		require.NoError(t, replayErr)
		require.Equal(t, uuid.NewSHA1(source, []byte("start-continuation")), child.ID)
		require.Equal(t, child.ID, replayed.ID)
	}
	original, err := s.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", original.State, "a local failure must not discard the deferred source")
	require.JSONEq(t, `{"start_continuation":"/profile"}`, string(original.Request))
}

func TestRecoverySecondHandoffReconcilesCurrentObserverDespiteOldTarget(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	parent, first := routedInput(t, s)
	parent = runningRecoveryParent(t, s, parent)
	second := accepted(t, s, 3)
	second, err := s.Route(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, parent.ID, second.TargetTaskID)
	first, err = s.Claim(ctx, first.ID, "first-observer", time.Minute)
	require.NoError(t, err)
	first.RunID = "successor-one"
	first, owns, err := s.OwnRecoveredRun(ctx, first, "first-observer", json.RawMessage(
		`{"run":{"replaces_run_id":"predecessor"},"recovery":{"superseded_run_id":"predecessor"}}`))
	require.NoError(t, err)
	require.True(t, owns)
	second, err = s.Claim(ctx, second.ID, "second-observer", time.Minute)
	require.NoError(t, err)
	second.RunID = "successor-two"
	snapshot := json.RawMessage(`{"run":{"replaces_run_id":"successor-one"},
	 "recovery":{"superseded_run_id":"successor-one"}}`)
	first.State, first.Snapshot = "awaiting_service", json.RawMessage(`{"reauthorizing":true}`)
	first, err = s.Save(ctx, first, "first-observer", first.Fence, nil)
	require.NoError(t, err)
	_, _, err = s.OwnRecoveredRun(ctx, second, "second-observer", snapshot)
	require.ErrorIs(t, err, db.ErrRoutingPending, "supersession cannot bypass unknown authorization")
	first.Snapshot = json.RawMessage(`{}`)
	first, err = s.Save(ctx, first, "first-observer", first.Fence, nil)
	require.NoError(t, err)
	second, owns, err = s.OwnRecoveredRun(ctx, second, "second-observer", snapshot)
	require.NoError(t, err)
	require.True(t, owns)
	_, err = s.Save(ctx, first, "first-observer", first.Fence, nil)
	require.ErrorIs(t, err, db.ErrLeaseLost)
	next := accepted(t, s, 4)
	next, err = s.Route(ctx, next.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, next.TargetTaskID)
}

func TestRecoveryDeliveredSteeringRetargetsDeferredWorkToItsActualObserver(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	parent, observer := routedInput(t, s)
	runningRecoveryParent(t, s, parent)
	input := accepted(t, s, 3)
	input, err := s.Route(ctx, input.ID)
	require.NoError(t, err)
	late := accepted(t, s, 4)
	late, err = s.Route(ctx, late.ID)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(ctx, `UPDATE shuttle_tasks SET request='{"start_continuation":"/profile"}' WHERE id=$1`, input.ID)
	require.NoError(t, err)
	observer, err = s.Claim(ctx, observer.ID, "observer", time.Minute)
	require.NoError(t, err)
	observer.RunID = "successor"
	observer, owns, err := s.OwnRecoveredRun(ctx, observer, "observer", json.RawMessage(
		`{"run":{"replaces_run_id":"predecessor"},"recovery":{"superseded_run_id":"predecessor"}}`))
	require.NoError(t, err)
	require.True(t, owns)
	input, err = s.Claim(ctx, input.ID, "steering", time.Minute)
	require.NoError(t, err)
	input.RunID = observer.RunID
	input, owns, err = s.OwnRecoveredRun(ctx, input, "steering", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.False(t, owns)
	require.Equal(t, observer.ID, input.TargetTaskID)
	deferred, err := s.HasDeferredStartForTask(ctx, observer.ID)
	require.NoError(t, err)
	require.True(t, deferred)
	observer.State = "completed"
	observer, err = s.Save(ctx, observer, "observer", observer.Fence, nil)
	require.NoError(t, err)
	late, err = s.Claim(ctx, late.ID, "late-receipt", time.Minute)
	require.NoError(t, err)
	late.RunID = observer.RunID
	late, owns, err = s.OwnRecoveredRun(ctx, late, "late-receipt", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.False(t, owns)
	require.Equal(t, observer.ID, late.TargetTaskID, "a late receipt retains the completed observer")
	stored, err := s.GetTask(ctx, late.ID)
	require.NoError(t, err)
	require.Equal(t, observer.ID, stored.TargetTaskID)
}
