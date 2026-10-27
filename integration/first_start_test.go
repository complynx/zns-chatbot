package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func seedNamedTelegramUser(t *testing.T, h *harness) {
	t.Helper()
	id, _ := registrationUser(t, h, 456, false)
	changeProfile(t, h, ownerProfileScope(t, h, id), personName(0, "Existing", "Person"))
}

func TestFirstStartOnboardingContinuesAcrossRestartAndThenUsesNativeMenu(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	start := h.submitText(t, 91401, "/start")
	h.await(t, start, "awaiting_user")
	deliveredNamePrompt(t, h, start)
	scope, pending, token := agentRequestsName(t, h, start)
	h.stop(t)
	h.start(t)
	answer := h.submitText(t, 91402, "Anna Maria de la Cruz")
	h.await(t, answer, "completed")
	h.await(t, start, "completed")
	unchanged, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Empty(t, unchanged.FullName, "A scripted provider cannot infer and save the answer")
	code, raw := profileRequest(t, h, token, "complete_name", uuid.New(), map[string]any{
		"expected_version": pending.Version, "expectation_id": pending.NameExpectation.ID,
		"first_name": "Anna Maria", "last_name": "de la Cruz",
	})
	require.Equal(t, 200, code, string(raw))
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	menu := h.submitText(t, 91403, "/start")
	h.await(t, menu, "completed")
	require.Equal(t, start, h.submitText(t, 91401, "/start"), "Replay must preserve the original accepted task")
	deliveredNamePrompt(t, h, menu)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "Anna Maria de la Cruz", promptUserText(p.inputs[0].Text))
	require.Contains(t, p.createdInputs[0], `"needs_real_name":true`)
	require.Contains(t, p.sent[len(p.sent)-1].Text, "Hi! I’m ZiNuSya")
	require.Contains(t, p.sent[len(p.sent)-1].Text, "/profile")
	require.Zero(t, p.loomUnavailableCalls)
}

func TestFirstStartWaitsForLoomAndUsesProfileCompletedWhileWaiting(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	start := h.submitText(t, 91501, "/start")
	h.await(t, start, "awaiting_authorization")
	scope, pending, token := agentRequestsName(t, h, start)
	code, raw := profileRequest(t, h, token, "complete_name", uuid.New(), map[string]any{
		"expected_version": pending.Version, "expectation_id": pending.NameExpectation.ID,
		"first_name": "Already", "last_name": "Completed",
	})
	require.Equal(t, 200, code, string(raw))
	require.Equal(t, start, h.submitText(t, 91501, "/start"))
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, start, "completed")
	profile, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, "Already Completed", profile.FullName)
	require.Nil(t, profile.NameExpectation)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], `"needs_real_name":false`)
}

func TestFirstStartReusesVerifiedExistingIAMName(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.users["existing-owner"] = map[string]any{
		"userId": "existing-owner", "state": "USER_STATE_ACTIVE",
		"details": map[string]any{"resourceOwner": "org"},
		"human": map[string]any{
			"profile": map[string]any{"givenName": "Anna Maria", "familyName": "de la Cruz"},
			"email":   map[string]any{"email": "tg+123+456@telegram.invalid", "isVerified": true},
		},
	}
	h := setup(t, p)
	id := h.submitText(t, 91601, "/start")
	h.await(t, id, "completed")
	task, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	profile, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, task.UserID))
	require.NoError(t, err)
	require.Equal(t, "Anna Maria de la Cruz", profile.FullName)
	require.Equal(t, "iam", profile.NameSource)
	require.Nil(t, profile.NameExpectation)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], `"needs_real_name":false`)
}
