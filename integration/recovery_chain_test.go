package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

type recoveryChainPeer struct {
	p        *peers
	active   string
	entered  chan struct{}
	release  chan struct{}
	gate     sync.Once
	receipts map[string]loom.RecoveryReceipt
}

func (f *recoveryChainPeer) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/chats/retained-chat/messages" {
		f.gate.Do(func() { close(f.entered) })
		select {
		case <-f.release:
			f.deliver(w, r)
		case <-r.Context().Done():
		}
		return
	}
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	located := loom.ActiveRun{Status: "idle", NextAction: "create_run"}
	if f.active != "" {
		located = loom.ActiveRun{RunID: f.active, Status: "accepted",
			Reason: "budget_exhausted", NextAction: "ask_user_to_continue"}
		if f.p.runs[f.active].Status == "completed" {
			located.Status, located.Reason, located.NextAction = "terminal", "", "send_message"
		}
	}
	if respondRecoveryDiscovery(w, r, located) {
		return
	}
	f.p.mu.Unlock()
	f.p.agent(w, r)
	f.p.mu.Lock()
}

func (f *recoveryChainPeer) deliver(w http.ResponseWriter, r *http.Request) {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	key := r.Header.Get("Idempotency-Key")
	if receipt, exists := f.receipts[key]; exists {
		respond(w, receipt)
		return
	}
	index := len(f.receipts) + 1
	run := loom.Run{ID: fmt.Sprintf("successor-%d", index), ChatID: "retained-chat", AuthSessionID: "session",
		Status: "completed", Result: fmt.Sprintf("Result %d", index), Interaction: loom.Interaction{ReplyMode: "agent"}}
	for id, previous := range f.p.runs {
		if previous.ID == f.active && !finishedPeerRun(previous.Status) {
			run.ReplacesRunID = previous.ID
			previous.Status, previous.Reason = "cancelled", "superseded"
			f.p.runs[id] = previous
		}
	}
	if index == 1 {
		run.Status, run.Reason, run.Result = "paused", "budget_exhausted", ""
	}
	f.p.runs[run.ID], f.active = run, run.ID
	receipt := loom.RecoveryReceipt{Accepted: true, Delivered: true, RunID: run.ID, MessageID: key,
		Sequence: int64(index), Status: "queued", NextAction: "read_run"}
	f.receipts[key] = receipt
	respond(w, receipt)
}

func finishedPeerRun(status string) bool {
	return status == "completed" || status == "cancelled"
}

func TestRecoveryQueuedMessageReplacesCurrentSuccessorAndKeepsProgress(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	f := &recoveryChainPeer{p: p, entered: make(chan struct{}), release: make(chan struct{}),
		receipts: map[string]loom.RecoveryReceipt{}}
	p.loom = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(p.loom.Close)
	h := setup(t, p)
	parent := h.submit(t, 901)
	awaitLoomRun(t, h, parent)
	row, err := h.store.GetTask(t.Context(), parent)
	require.NoError(t, err)
	p.mu.Lock()
	initial := p.runs[parent.String()]
	initial.Status, initial.Reason, initial.ChatID = "paused", "budget_exhausted", "retained-chat"
	p.runs[parent.String()], f.active = initial, row.RunID
	p.mu.Unlock()
	first := h.submitText(t, 902, "Continue once")
	select {
	case <-f.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first recovery message did not reach Loom")
	}
	second := h.submitText(t, 903, "Continue again")
	require.Eventually(t, func() bool {
		queued, readErr := h.store.GetTask(t.Context(), second)
		return readErr == nil && queued.TargetTaskID == parent
	}, 10*time.Second, 20*time.Millisecond)
	close(f.release)
	h.await(t, second, "completed")
	h.await(t, first, "closed")
	h.await(t, parent, "closed")
	p.mu.Lock()
	p.steering = false
	p.mu.Unlock()
	third := h.submitText(t, 904, "Next request")
	h.await(t, third, "completed")
	deliveredNamePrompt(t, h, second)
	deliveredNamePrompt(t, h, third)
	h.stop(t)
	current, err := h.store.GetTask(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, "successor-2", current.RunID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, f.receipts, 2)
	require.Len(t, p.createdInputs, 2)
	results := map[string]int{}
	for _, message := range p.sent {
		results[message.Text]++
	}
	require.Equal(t, 1, results["Result 2"])
}
