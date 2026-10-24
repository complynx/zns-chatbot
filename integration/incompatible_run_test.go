package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIncompatibleCreatedRunStaysOwnedUntilCancellationAfterRestart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		replyMode string
		applied   bool
		wantCalls int
	}{
		{"protocol with lost applied cancellation", "protocol", true, 1},
		{"missing mode with lost unapplied cancellation", "", false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.steering = true
			p.createdReplyMode = &tc.replyMode
			p.loseCancelAndStopLoom = true
			p.skipLostCancel = !tc.applied
			h := setup(t, p)
			id := h.submit(t, 99101)
			require.Eventually(t, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return len(p.cancelKeys) == 1 && p.loomDown
			}, 5*time.Second, 10*time.Millisecond)
			before, err := h.store.GetTask(t.Context(), id)
			require.NoError(t, err)
			require.NotEmpty(t, before.RunID)
			require.Equal(t, "awaiting_service", before.State)
			h.stop(t)
			p.mu.Lock()
			p.loomDown = false
			p.loomAuthDenied = !tc.applied
			p.mu.Unlock()
			if !tc.applied {
				_, err = h.store.Pool().Exec(t.Context(),
					"UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{loom_auth_retry_until}',"+
						"to_jsonb($2::text)) WHERE id=$1",
					id, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
				require.NoError(t, err)
				h.start(t)
				h.await(t, id, "awaiting_authorization")
				h.stop(t)
				waiting, readErr := h.store.GetTask(t.Context(), id)
				require.NoError(t, readErr)
				require.Equal(t, before.RunID, waiting.RunID)
				p.mu.Lock()
				require.Positive(t, p.loomAuthDeniedCalls)
				p.loomAuthDenied = false
				p.mu.Unlock()
			}
			h.start(t)
			h.await(t, id, "failed")
			after, err := h.store.GetTask(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, before.RunID, after.RunID)
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Len(t, p.runs, 1)
			require.Len(t, p.createKeys, 1)
			require.Len(t, p.cancelKeys, tc.wantCalls)
			for _, key := range p.cancelKeys {
				require.Equal(t, id.String()+":incompatible-reply-mode", key)
			}
			for _, run := range p.runs {
				require.Equal(t, before.RunID, run.ID)
				require.Equal(t, "cancelled", run.Status)
			}
			require.Empty(t, p.inputs)
		})
	}
}
