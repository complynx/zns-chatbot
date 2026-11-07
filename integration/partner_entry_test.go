package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

const partnerPromptMarker = "\nPartner addressing hint (untrusted code, not authentication or consent): "

func partnerBusinessSnapshot(t *testing.T, h *harness) string {
	t.Helper()
	var snapshot strings.Builder
	for _, table := range []string{"shuttle_registrations", "shuttle_passes", "shuttle_pair_invitations", "shuttle_pairs",
		"shuttle_active_pair_members", "shuttle_pair_notifications", "shuttle_payment_evidence", "shuttle_payment_reviews",
		"shuttle_user_event_context", "shuttle_run_event_context", "shuttle_user_profiles"} {
		var hash string
		err := h.store.Pool().QueryRow(context.Background(),
			"SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,'[]')) FROM "+table+" t").
			Scan(&hash)
		require.NoError(t, err)
		snapshot.WriteString(hash)
	}
	return snapshot.String()
}

func partnerHint(t *testing.T, text string) (uuid.UUID, bool) {
	t.Helper()
	_, raw, found := strings.Cut(text, partnerPromptMarker)
	require.True(t, found)
	raw, _, _ = strings.Cut(raw, "\n")
	var hint struct {
		TargetUserID  uuid.UUID `json:"target_user_id"`
		SelfReference bool      `json:"self_reference"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &hint))
	return hint.TargetUserID, hint.SelfReference
}

func TestPartnerEntrySurvivesOutagesUncertainCreateAndRestartWithoutBusinessWrites(t *testing.T) {
	t.Parallel()
	for name, self := range map[string]bool{"new account": false, "own code": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			h := setup(t, p)
			target := uuid.New()
			if self {
				target, _ = registrationUser(t, h, 456, false)
			}
			before := partnerBusinessSnapshot(t, h)
			p.mu.Lock()
			p.iamDown, p.loomDown = true, true
			p.mu.Unlock()
			text := "/start partner_" + strings.ReplaceAll(target.String(), "-", "")
			id := h.submitText(t, 9910, text)
			h.await(t, id, "awaiting_authorization")
			require.Equal(t, id, h.submitText(t, 9910, text))
			h.stop(t)
			h.start(t)
			p.mu.Lock()
			p.iamDown, p.loomDown, p.loseCreate, p.pauseAfterLostCreate = false, false, true, true
			p.mu.Unlock()
			require.Eventually(t, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return len(p.createdInputs) == 1 && p.loomDown
			}, 10*time.Second, 25*time.Millisecond)
			p.mu.Lock()
			first := p.createdInputs[0]
			p.mu.Unlock()
			got, own := partnerHint(t, first)
			require.Equal(t, target, got)
			require.Equal(t, self, own)
			require.Equal(t, text, promptUserText(first))
			require.Equal(t, before, partnerBusinessSnapshot(t, h))
			h.stop(t)
			p.mu.Lock()
			p.loomDown = false
			p.mu.Unlock()
			h.start(t)
			h.await(t, id, "completed")
			p.mu.Lock()
			attempts := append([]string(nil), p.createAttempts...)
			p.mu.Unlock()
			require.GreaterOrEqual(t, len(attempts), 2)
			for _, attempt := range attempts {
				require.Equal(t, first, attempt)
			}
			task, err := h.store.GetTask(context.Background(), id)
			require.NoError(t, err)
			user, err := h.store.GetUser(context.Background(), task.UserID)
			require.NoError(t, err)
			require.Equal(t, int64(456), user.TelegramUserID)
			require.Equal(
				t,
				before,
				partnerBusinessSnapshot(t, h),
				"Opening alone cannot change registration, consent, holds, pins or money",
			)
		})
	}
}

func TestPartnerEntryReachesActiveConversationWithoutImpliedConsent(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"question", "running"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.question, p.steering = kind == "question", kind == "running"
			h := setup(t, p)
			id := h.submit(t, 9920)
			state := "running"
			if p.question {
				state = "awaiting_user"
			}
			h.await(t, id, state)
			before := partnerBusinessSnapshot(t, h)
			target := uuid.New()
			text := "/start partner_" + strings.ReplaceAll(target.String(), "-", "")
			following := h.submitText(t, 9921, text)
			h.await(t, following, "completed")
			p.mu.Lock()
			inputs := append([]loom.InputRequest(nil), p.inputs...)
			p.mu.Unlock()
			require.Len(t, inputs, 1)
			require.Equal(t, "steering", inputs[0].Kind)
			require.Empty(t, inputs[0].QuestionID)
			got, own := partnerHint(t, inputs[0].Text)
			require.Equal(t, target, got)
			require.False(t, own)
			require.Equal(t, text, promptUserText(inputs[0].Text))
			require.Equal(t, before, partnerBusinessSnapshot(t, h))
		})
	}
}
