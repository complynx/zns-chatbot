package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestRecoveredRunAllowanceAndContextBlockersAreActionable(t *testing.T) {
	t.Parallel()
	cases := []struct{ reason, note, expected string }{
		{"budget_exhausted", "", "work allowance"},
		{"budget_exhausted", "Finished the first check.", "Finished the first check."},
		{"credits_exhausted", "", "Add funds"},
		{"recovery_context_unavailable", "", "stop the previous run safely"},
	}
	for _, tc := range cases {
		t.Run(tc.reason+tc.note, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.steering = true
			h := setup(t, p)
			id := h.submit(t, 301)
			awaitLoomRun(t, h, id)
			h.stop(t)
			pauseMockRun(t, h, id, tc.reason)
			p.mu.Lock()
			run := p.runs[id.String()]
			run.Result = tc.note
			p.runs[id.String()] = run
			p.mu.Unlock()
			_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot ||
			 jsonb_build_object('recovery',jsonb_build_object('chat_id','retained-chat','receipt',
			 jsonb_build_object('delivered',true,'run_id',run_id))) WHERE id=$1`, id)
			require.NoError(t, err)
			h.start(t)
			h.awaitReplies(t, 1)
			awaitRecoveryDeliverySettled(t, h, id)
			h.stop(t)
			p.mu.Lock()
			require.Contains(t, p.sent[0].Text, tc.expected)
			if tc.reason == "budget_exhausted" {
				require.IsType(t, &telegram.ReplyKeyboardMarkup{}, p.sent[0].ReplyMarkup)
			} else {
				require.Nil(t, p.sent[0].ReplyMarkup)
			}
			require.Len(t, p.createdInputs, 1)
			p.mu.Unlock()
			if tc.reason == "credits_exhausted" {
				assertRepeatedFundsPause(t, h, p, id)
			}
		})
	}
}

func assertRepeatedFundsPause(t *testing.T, h *harness, p *peers, id uuid.UUID) {
	t.Helper()
	p.mu.Lock()
	run := p.runs[id.String()]
	run.Status, run.Reason = "running", ""
	run.Version++
	p.runs[id.String()] = run
	p.mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		current, err := h.store.GetTask(t.Context(), id)
		var snapshot struct {
			Run loom.Run `json:"run"`
		}
		return err == nil && json.Unmarshal(current.Snapshot, &snapshot) == nil &&
			snapshot.Run.Status == "running" && snapshot.Run.Version == run.Version
	}, 10*time.Second, 20*time.Millisecond)
	pauseMockRun(t, h, id, "credits_exhausted")
	h.awaitReplies(t, 2)
	awaitRecoveryDeliverySettled(t, h, id)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[1].Text, "Add funds")
	require.Len(t, p.createdInputs, 1, "a repeated wallet pause must not create a paid successor")
}

func TestAllowanceProgressNoteRechecksIAMBeforeRetryDelivery(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 302)
	awaitLoomRun(t, h, id)
	h.stop(t)
	pauseMockRun(t, h, id, "budget_exhausted")
	p.mu.Lock()
	run := p.runs[id.String()]
	run.Result = "Private progress note"
	p.runs[id.String()] = run
	p.rejectSendOnce = true
	p.mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		rows, err := h.store.ListOutbox(t.Context(), id)
		return err == nil && len(rows) > 0 && rows[0].State == "retryable"
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	require.NoError(t, h.iam.SetPositiveCacheTTL(0))
	_, err := h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET snapshot=snapshot ||
	 jsonb_build_object('recovery',jsonb_build_object('chat_id','retained-chat','receipt',
	 jsonb_build_object('delivered',true,'run_id',run_id))) WHERE id=$1`, id)
	require.NoError(t, err)
	p.mu.Lock()
	p.userInactive = true
	p.mu.Unlock()
	h.start(t)
	require.Eventually(t, func() bool {
		rows, checkErr := h.store.ListOutbox(t.Context(), id)
		return checkErr == nil && len(rows) > 0 && rows[0].State == "delivery_blocked"
	}, 10*time.Second, 20*time.Millisecond)
	h.stop(t)
	current, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	_, err = h.store.SetUserRevoked(t.Context(), current.UserID, "test-admin")
	require.NoError(t, err)
	h.start(t)
	h.awaitReplies(t, 2)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	attempts := 0
	for _, sent := range p.sent {
		if strings.Contains(sent.Text, "Private progress note") {
			attempts++
		}
	}
	require.Equal(t, 1, attempts, "the rejected send must not repeat after current access is denied")
	denials := 0
	for _, sent := range p.sent {
		if strings.Contains(sent.Text, "contact the administrator") {
			denials++
		}
	}
	require.Equal(t, 1, denials, "the static access-denied notice stays deliverable")
}
