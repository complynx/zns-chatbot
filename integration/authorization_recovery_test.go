package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLoomAuthorizationRecoveryAfterRestartKeepsSameRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submitText(t, 90, "Recover authorization")
	awaitLoomRun(t, h, id)
	before, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	h.stop(t)
	p.mu.Lock()
	p.loomAuthDenied = true
	p.mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.loomAuthDeniedCalls > 0
	}, 5*time.Second, 20*time.Millisecond)
	waiting, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.NotEqual(t, "failed", waiting.State)
	require.Equal(t, before.RunID, waiting.RunID)
	h.stop(t)
	h.start(t)
	p.mu.Lock()
	p.loomAuthDenied = false
	run := p.runs[id.String()]
	run.Status, run.Result = "completed", "Recovered after restart"
	p.runs[id.String()] = run
	p.mu.Unlock()
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	after, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, before.RunID, after.RunID)
	require.Equal(t, before.AuthSessionID, after.AuthSessionID)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Equal(t, "Recovered after restart", p.sent[0].Text)
}

func TestLoomAuthorizationDeadlineRetainsRunCustodyAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, lost := range []bool{false, true} {
		name := "known_run"
		if lost {
			name = "lost_creation_receipt"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testAuthorizationDeadlineRetainsRun(t, lost)
		})
	}
}

func testAuthorizationDeadlineRetainsRun(t *testing.T, lost bool) {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	p.loseCreate, p.pauseAfterLostCreate = lost, lost
	h := setup(t, p)
	id := h.submit(t, 91)
	if lost {
		h.await(t, id, "awaiting_service")
	} else {
		awaitLoomRun(t, h, id)
	}
	h.stop(t)
	p.mu.Lock()
	p.loomAuthDenied = true
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "awaiting_authorization")
	h.stop(t)
	before, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	// Model a long outage while the process was stopped, retaining its deadline.
	_, err = h.store.Pool().Exec(context.Background(),
		`UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{loom_auth_retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	p.mu.Lock()
	deniedCalls := p.loomAuthDeniedCalls
	p.mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.loomAuthDeniedCalls > deniedCalls
	}, 5*time.Second, 20*time.Millisecond)
	h.stop(t)
	waiting, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "awaiting_authorization", waiting.State)
	require.Equal(t, before.RunID, waiting.RunID)
	require.Equal(t, before.AuthSessionID, waiting.AuthSessionID)
	var owner uuid.UUID
	require.NoError(t, h.store.Pool().QueryRow(context.Background(),
		`SELECT active_task_id FROM shuttle_conversations WHERE id=$1`, before.ConversationID).Scan(&owner))
	require.Equal(t, id, owner)
	// New text must continue the retained task, rather than start a parallel run.
	h.start(t)
	next := h.submitText(t, 9191, "Continue the same task")
	routed, err := h.store.Route(context.Background(), next)
	require.NoError(t, err)
	require.Equal(t, id, routed.TargetTaskID)
	require.Equal(t, "input", routed.Route)
	h.stop(t)
	p.mu.Lock()
	runCount, replyCount := len(p.runs), len(p.sent)
	p.loomAuthDenied = false
	p.mu.Unlock()
	require.Equal(t, 1, runCount)
	require.Zero(t, replyCount)
	h.start(t)
	awaitLoomRun(t, h, id)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.inputs) == 1
	}, 5*time.Second, 20*time.Millisecond)
	h.stop(t)
	after, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, before.AuthSessionID, after.AuthSessionID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, p.runs[id.String()].ID, after.RunID)
	require.Len(t, p.runs, 1)
	for _, key := range p.createKeys {
		require.Equal(t, id.String(), key)
	}
}

func TestLocalRevocationStopsLoomAuthorizationRecovery(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 92)
	awaitLoomRun(t, h, id)
	h.stop(t)
	p.mu.Lock()
	p.loomAuthDenied = true
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "awaiting_authorization")
	accepted, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	_, err = h.store.SetUserRevoked(context.Background(), accepted.UserID, "test-admin")
	require.NoError(t, err)
	h.await(t, id, "failed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Empty(t, p.inputs)
	require.Equal(t, "Access denied. Please contact the administrator.", p.sent[0].Text)
}

func TestLoomAuthorizationDeadlineBeforeCreationStillDenies(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomAuthDenied = true
	h := setup(t, p)
	id := h.submit(t, 9192)
	h.await(t, id, "awaiting_authorization")
	h.stop(t)
	_, err := h.store.Pool().Exec(context.Background(),
		`UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{loom_auth_retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	h.start(t)
	h.await(t, id, "failed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Equal(t, "Access denied. Please contact the administrator.", p.sent[0].Text)
}

func TestLostReauthorizationReceiptIsNotRepeatedAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 93)
	awaitLoomRun(t, h, id)
	h.stop(t)
	_, err := h.store.Pool().Exec(context.Background(),
		`UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{session,expires_at}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	p.mu.Lock()
	p.loseReauthorization = true
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "auth_unknown")
	h.awaitReplies(t, 1)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submit(t, 93))
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Len(t, p.sent, 1)
	require.Len(t, p.runs, 1)
}

func TestLoomAuthorizationRecoveryBeforeBootstrapKeepsAcceptedWork(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomAuthDenied = true
	h := setup(t, p)
	id := h.submit(t, 94)
	h.await(t, id, "awaiting_authorization")
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.loomAuthDeniedCalls > 0
	}, 5*time.Second, 20*time.Millisecond)
	p.mu.Lock()
	p.loomAuthDenied = false
	p.mu.Unlock()
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Equal(t, []string{id.String()}, p.createKeys)
	require.Equal(t, 0, p.sessionReauthorizations)
}

func awaitLoomRun(t *testing.T, h *harness, id uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(context.Background(), id)
		return err == nil && current.State == "running" && current.RunID != ""
	}, 5*time.Second, 20*time.Millisecond)
}

func TestServiceOutageDoesNotConsumeLoomAuthorizationGrace(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 95)
	awaitLoomRun(t, h, id)
	h.stop(t)
	p.mu.Lock()
	p.loomAuthDenied = true
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "awaiting_authorization")
	h.stop(t)
	_, err := h.store.Pool().Exec(context.Background(),
		`UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{loom_auth_retry_until}',to_jsonb($2::text)) WHERE id=$1`,
		id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "awaiting_service")
	p.mu.Lock()
	p.loomDown = false
	p.loomAuthDenied = false
	run := p.runs[id.String()]
	run.Status, run.Result = "completed", "Recovered after service outage"
	p.runs[id.String()] = run
	p.mu.Unlock()
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Equal(t, "Recovered after service outage", p.sent[0].Text)
}

func TestSameExpiryCredentialKeepsValidLoomSession(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	p.loseReauthorization = true
	p.getEntered = make(chan struct{}, 1)
	release := make(chan struct{})
	p.getRelease = release
	var once sync.Once
	defer once.Do(func() { close(release) })
	h := setup(t, p)
	p.mu.Lock()
	// Both the token and bootstrap receipt remain valid within the renewal window.
	// Reissuing this credential cannot extend the existing session.
	p.tokenExpiry = time.Now().Add(10 * time.Second).UTC().Truncate(time.Second)
	p.mu.Unlock()
	id := h.submitText(t, 196, "Continue with the valid session")
	select {
	case <-p.getEntered:
	case <-time.After(3 * time.Second):
		current, err := h.store.GetTask(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, "running", current.State)
		t.Fatal("valid session did not reach its Loom run read")
	}
	before, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	p.mu.Lock()
	run := p.runs[id.String()]
	run.Status, run.Result = "completed", "Kept valid authorization"
	p.runs[id.String()] = run
	p.mu.Unlock()
	once.Do(func() { close(release) })
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	after, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, before.RunID, after.RunID)
	require.Equal(t, before.AuthSessionID, after.AuthSessionID)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.sessionReauthorizations)
	require.Len(t, p.runs, 1)
	require.Len(t, p.sent, 1)
	require.Equal(t, "Kept valid authorization", p.sent[0].Text)
}
