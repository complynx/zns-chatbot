package db_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

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
		json.RawMessage(`{"recovery":{"receipt":{"delivered":true}}}`),
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
	input, err := s.Claim(ctx, input.ID, "recovery-worker", time.Minute)
	require.NoError(t, err)
	input.RunID = parent.RunID
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
	input.State = "completed"
	input.AuthSessionID = parent.AuthSessionID
	input.Snapshot = json.RawMessage(`{"session":{"auth_session_id":"session","version":3}}`)
	_, err = s.Save(ctx, input, "recovery-worker", input.Fence, nil)
	require.NoError(t, err)
	snapshot, err := s.RecoveryRunSession(ctx, parent.UserID, "successor", parent.AuthSessionID)
	require.NoError(t, err)
	require.JSONEq(t, string(input.Snapshot), string(snapshot), "handoff must retain the newest session receipt")
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
