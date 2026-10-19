package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func suspendRunAuthorization(t *testing.T, h *harness, reason string) (uuid.UUID, db.Task) {
	t.Helper()
	id := h.submitText(t, 991, "Recover the original request")
	awaitLoomRun(t, h, id)
	h.stop(t)
	before, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	h.peers.mu.Lock()
	run := h.peers.runs[id.String()]
	run.Status, run.Reason = "waiting_auth", reason
	h.peers.runs[id.String()] = run
	h.peers.mu.Unlock()
	return id, before
}

func TestWaitingAuthRenewsUnexpiredSessionAndKeepsOriginalRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id, before := suspendRunAuthorization(t, h, "authorization_required")
	h.start(t)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	after, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, before.RunID, after.RunID)
	require.Equal(t, before.AuthSessionID, after.AuthSessionID)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Equal(t, []int64{1}, p.reauthorizationVersions)
	require.Len(t, p.runs, 1)
	require.Empty(t, p.resumeKeys)
	require.Len(t, p.sent, 1)
}

func TestWaitingAuthLostRenewalReceiptDoesNotRepeatAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id, _ := suspendRunAuthorization(t, h, "authorization_required")
	p.mu.Lock()
	p.loseReauthorization = true
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "auth_unknown")
	h.awaitReplies(t, 1)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submitText(t, 991, "Recover the original request"))
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Empty(t, p.resumeKeys)
	require.Len(t, p.runs, 1)
	require.Len(t, p.sent, 1)
}

func TestWaitingAuthPermissionRevocationDoesNotRenew(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id, _ := suspendRunAuthorization(t, h, "permission_revoked")
	h.start(t)
	h.await(t, id, "failed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.sessionReauthorizations)
	require.Empty(t, p.resumeKeys)
	require.Len(t, p.runs, 1)
}

func TestWaitingAuthSuccessfulRenewalDoesNotResetGraceDeadline(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.keepWaitingAuth = true, true
	h := setup(t, p)
	id, _ := suspendRunAuthorization(t, h, "authorization_required")
	h.start(t)
	var originalDeadline *time.Time
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(context.Background(), id)
		if err != nil {
			return false
		}
		var saved struct {
			Deadline *time.Time `json:"loom_auth_retry_until"`
		}
		if json.Unmarshal(current.Snapshot, &saved) != nil {
			return false
		}
		originalDeadline = saved.Deadline
		return originalDeadline != nil
	}, 5*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.sessionReauthorizations >= 2
	}, 5*time.Second, 20*time.Millisecond)
	h.stop(t)
	current, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	var saved struct {
		Deadline *time.Time `json:"loom_auth_retry_until"`
	}
	require.NoError(t, json.Unmarshal(current.Snapshot, &saved))
	require.NotNil(t, saved.Deadline)
	require.Equal(t, originalDeadline, saved.Deadline)
}

func TestWaitingAuthExpiredGraceSurvivesRestartWithoutRenewal(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.keepWaitingAuth = true, true
	h := setup(t, p)
	id, _ := suspendRunAuthorization(t, h, "authorization_required")
	_, err := h.store.Pool().Exec(context.Background(),
		`UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{loom_auth_retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	h.start(t)
	h.await(t, id, "failed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.sessionReauthorizations)
	require.Empty(t, p.resumeKeys)
}

func TestWaitingAuthLocalRevocationStopsRecoveryBeforeRenewal(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id, before := suspendRunAuthorization(t, h, "authorization_required")
	_, err := h.store.SetUserRevoked(context.Background(), before.UserID, "fixture")
	require.NoError(t, err)
	h.start(t)
	h.await(t, id, "failed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.sessionReauthorizations)
	require.Empty(t, p.resumeKeys)
	require.Len(t, p.runs, 1)
}

func TestWaitingAuthFromLostCreatePinsRunBeforeRecovery(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.loseCreate, p.pauseAfterLostCreate = true, true, true
	h := setup(t, p)
	id := h.submitText(t, 992, "Recover creation receipt")
	h.await(t, id, "awaiting_service")
	h.stop(t)
	before, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Empty(t, before.RunID)
	p.mu.Lock()
	run := p.runs[id.String()]
	run.Status, run.Reason = "waiting_auth", "authorization_required"
	p.runs[id.String()] = run
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	after, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, run.ID, after.RunID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, []string{id.String(), id.String()}, p.createKeys)
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Positive(t, p.runGets)
	require.Empty(t, p.resumeKeys)
	require.Len(t, p.runs, 1)
}
