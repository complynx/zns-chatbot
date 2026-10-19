package integration_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestPreparedSteeringSurvivesRestartAfterParentFinishes(t *testing.T) {
	t.Parallel()
	for _, conflict := range []bool{false, true} {
		name := "typed_terminal_rejection"
		if conflict {
			name = "unrelated_conflict_stays_unresolved"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.steering, p.echoInput = true, true
			p.terminalInputConflict = conflict
			p.firstInputEntered = make(chan struct{})
			p.firstInputCanceled = make(chan struct{})
			release := make(chan struct{})
			p.firstInputRelease = release
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			h := setup(t, p)
			original := h.submitText(t, 70, "first message")
			h.await(t, original, "running")
			following := h.submitText(t, 71, "first")
			select {
			case <-p.firstInputEntered:
			case <-time.After(5 * time.Second):
				t.Fatal("following message never reached the prepared Input barrier")
			}
			prepared, err := h.store.GetTask(context.Background(), following)
			require.NoError(t, err)
			var snapshot struct {
				Control          *loom.InputRequest `json:"control"`
				ControlUnapplied bool               `json:"control_unapplied"`
			}
			require.NoError(t, json.Unmarshal(prepared.Snapshot, &snapshot))
			require.NotNil(t, snapshot.Control)
			require.Equal(t, "steering", snapshot.Control.Kind)
			require.Equal(t, "first", promptUserText(snapshot.Control.Text))
			require.False(t, snapshot.ControlUnapplied)
			p.mu.Lock()
			inputEffects := len(p.inputs)
			p.mu.Unlock()
			require.Zero(t, inputEffects)
			h.stop(t)
			select {
			case <-p.firstInputCanceled:
			case <-time.After(5 * time.Second):
				t.Fatal("old Input handler did not observe cancellation")
			}
			p.mu.Lock()
			run := p.runs[original.String()]
			run.Status, run.Result = "completed", "first message"
			run.Version++
			p.runs[original.String()] = run
			p.steering = false
			p.mu.Unlock()
			releaseOnce.Do(func() { close(release) })
			h.start(t)
			require.Equal(t, following, h.submitText(t, 71, "first"))
			h.await(t, original, "completed")
			if conflict {
				h.await(t, following, "awaiting_service")
				require.Eventually(
					t,
					func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.inputAttempts) >= 2 },
					5*time.Second,
					10*time.Millisecond,
				)
				h.awaitReplies(t, 1)
			} else {
				h.await(t, following, "completed")
				h.awaitReplies(t, 2)
			}
			h.stop(t)
			recovered, err := h.store.GetTask(context.Background(), following)
			require.NoError(t, err)
			require.JSONEq(t, string(prepared.Request), string(recovered.Request))
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Empty(t, p.inputs)
			require.GreaterOrEqual(t, len(p.inputAttempts), 2)
			for idx, input := range p.inputAttempts {
				require.Equal(t, following.String(), p.inputKeys[idx])
				require.Equal(t, *snapshot.Control, input, "recovery must reuse the frozen request")
			}
			if conflict {
				require.Equal(t, "awaiting_service", recovered.State)
				require.JSONEq(t, string(prepared.Snapshot), string(recovered.Snapshot))
				require.Len(t, p.runs, 1)
				require.Equal(t, []string{"first message"}, promptUserTexts(p.createdInputs))
			} else {
				require.Len(t, p.runs, 2)
				require.Equal(t, []string{"first message", "first"}, promptUserTexts(p.createdInputs))
				require.ElementsMatch(t, []string{"first message", "first"}, []string{p.sent[0].Text, p.sent[1].Text})
			}
		})
	}
}

func TestLostSteeringReceiptReconcilesSameInputAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.echoInput = true, true
	p.loseInputAndStopLoom = true
	h := setup(t, p)
	original := h.submitText(t, 72, "first message")
	h.await(t, original, "running")
	following := h.submitText(t, 73, "second message")
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.inputs) == 1 && p.loomDown
	}, 5*time.Second, 10*time.Millisecond)
	h.await(t, following, "awaiting_service")
	h.stop(t)
	prepared, err := h.store.GetTask(context.Background(), following)
	require.NoError(t, err)
	p.mu.Lock()
	run := p.runs[original.String()]
	run.Status, run.Result = "completed", "first message"
	run.Version++
	p.runs[original.String()] = run
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	require.Equal(t, following, h.submitText(t, 73, "second message"))
	h.await(t, original, "completed")
	h.await(t, following, "completed")
	h.awaitReplies(t, 1)
	h.stop(t)
	recovered, err := h.store.GetTask(context.Background(), following)
	require.NoError(t, err)
	require.JSONEq(t, string(prepared.Request), string(recovered.Request))
	require.Equal(t, "run-"+original.String(), recovered.RunID)
	outbox, err := h.store.ListOutbox(context.Background(), following)
	require.NoError(t, err)
	require.Empty(t, outbox, "a reconciled Input must not invent a no-active reply")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.GreaterOrEqual(t, len(p.inputAttempts), 2)
	require.Equal(t, []string{"first message"}, promptUserTexts(p.createdInputs))
	for idx, input := range p.inputAttempts {
		require.Equal(t, following.String(), p.inputKeys[idx])
		require.Equal(t, p.inputs[0], input)
	}
}
