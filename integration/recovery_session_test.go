package integration_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

type recoveryRenewalPeer struct {
	p         *peers
	runID     string
	runKey    string
	blocked   chan struct{}
	blockOnce sync.Once
}

func (f *recoveryRenewalPeer) serve(w http.ResponseWriter, r *http.Request) {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	switch r.URL.Path {
	case "/v1/capabilities":
		respondRecoveryCapabilities(w)
	case "/v1/chats/default":
		respond(w, map[string]string{"id": "retained-chat"})
	case "/v1/chats/retained-chat/active-run":
		located := loom.ActiveRun{Status: "idle", NextAction: "create_run"}
		if f.runID != "" {
			located = loom.ActiveRun{RunID: f.runID, Status: "blocked", Reason: "authorization_required",
				NextAction: "reauthorize_session"}
			if f.p.sessionReauthorizations > 0 {
				located.Status, located.Reason, located.NextAction = "terminal", "", "send_message"
			}
		}
		respond(w, located)
	case "/v1/chats/retained-chat/messages":
		receipt := loom.RecoveryReceipt{Accepted: true, MessageID: "retained-input", Sequence: 1,
			Status: "blocked", Reason: "authorization_required", NextAction: "reauthorize_session"}
		if f.p.sessionReauthorizations > 0 {
			receipt.Delivered, receipt.RunID, receipt.InputID = true, f.runID, "steering"
			receipt.Status, receipt.Reason, receipt.NextAction = "terminal", "", "read_run"
		}
		respond(w, receipt)
		f.blockOnce.Do(func() { close(f.blocked) })
	default:
		f.p.mu.Unlock()
		f.p.agent(w, r)
		f.p.mu.Lock()
	}
}

func TestRecoverySteeringLeavesSessionRenewalToItsLiveObserver(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	f := &recoveryRenewalPeer{
		p:       p,
		blocked: make(chan struct{}),
	}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	first := h.submit(t, 1101)
	awaitLoomRun(t, h, first)
	h.stop(t)
	row, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	p.mu.Lock()
	f.runID, f.runKey = row.RunID, first.String()
	run := p.runs[f.runKey]
	run.Status, run.Reason, run.ChatID = "waiting_auth", "recovery_authorization_required", "retained-chat"
	p.runs[f.runKey] = run
	p.mu.Unlock()
	// Keep the observer parked while the new input reaches the same session.
	// Advancing its queue job gives an ordering barrier without an HTTP timeout.
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE river_job SET state='scheduled',
	 scheduled_at=now()+interval '1 hour' WHERE args->>'task_id'=$1
	 AND state IN ('available','scheduled','retryable')`, first.String())
	require.NoError(t, err)
	h.start(t)
	second := h.submitText(t, 1102, "Keep working after authorization")
	select {
	case <-f.blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("recovery input did not reach the authorization gate")
	}
	require.Eventually(t, func() bool {
		current, readErr := h.store.GetTask(t.Context(), second)
		return readErr == nil && current.LeaseOwner == "" && len(current.Snapshot) > 2
	}, 10*time.Second, 10*time.Millisecond)
	p.mu.Lock()
	renewalsBeforeObserver := p.sessionReauthorizations
	p.mu.Unlock()
	require.Zero(t, renewalsBeforeObserver, "the input must not race the current observer's CAS renewal")
	_, err = h.store.Pool().Exec(t.Context(), `UPDATE river_job SET state='available',scheduled_at=now()
	 WHERE args->>'task_id'=$1 AND state='scheduled'`, first.String())
	require.NoError(t, err)
	h.await(t, first, "completed")
	h.await(t, second, "completed")
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.sessionReauthorizations)
	require.Equal(t, []int64{1}, p.reauthorizationVersions)
	require.Len(t, p.createdInputs, 1)
	for _, sent := range p.sent {
		require.NotContains(t, sent.Text, "Authorization must be renewed")
	}
}
