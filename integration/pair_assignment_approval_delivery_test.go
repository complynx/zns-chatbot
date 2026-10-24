package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func acceptPairApprovalRequest(t *testing.T, f pairAssignmentAppFixture, telegramID int64) uuid.UUID {
	t.Helper()
	update := telegram.Update{
		ID: 99481,
		Message: &telegram.Message{
			ID:   99481,
			From: &telegram.User{ID: telegramID, LanguageCode: "en"},
			Chat: telegram.Chat{ID: telegramID, Type: "private"},
			Text: "Show me the exact pair assignment for approval",
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	task, _, err := f.H.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	return task.ID
}

func TestPairAssignmentApprovalDisplaysAuthorizedOriginalPairBeforeConsent(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	configureTierPair(t, f.pairFixture, f.admin, &f.args, tierPolicy(registrations.PricingPaired, 10))
	_, err := f.H.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"approval-event-admin",
		db.AuthorityChange{EventID: f.EventID, UserID: f.admin.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	scoped := f.admin
	scoped.GlobalAdmin = false
	_ = pairUserToken(f.pairFixture, scoped)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), scoped, "original-pair-approval", f.args)
	require.NoError(t, err)
	// The current agent transport adopts an existing native-capable proposal.
	_, err = f.H.store.AdoptPairAssignment(t.Context(), scoped, "loom-pair-approval", f.args, prep.ID)
	require.NoError(t, err)
	pairIdentityName(t, f.pairFixture, f.A, "Alex ``` **literal**")
	raw, err := json.Marshal(f.args)
	require.NoError(t, err)
	f.H.peers.mu.Lock()
	f.H.peers.approval = &loom.Approval{
		ID:               "loom-pair-approval",
		OperationID:      "loom-pair-approval",
		Method:           "zinusya_pairs.assign",
		ReplyMode:        loom.ReplyModeAgent,
		Text:             "INCOMPLETE UNTRUSTED SUMMARY",
		ConfirmationText: "WRONG PERSON",
		Arguments:        raw,
		ArgumentHash:     "bound-action",
		ExpiresAt:        prep.ExpiresAt,
	}
	f.H.peers.mu.Unlock()
	id := acceptPairApprovalRequest(t, f, 12005)
	f.H.await(t, id, "awaiting_user")
	var sent []telegram.SendMessageRequest
	require.Eventually(t, func() bool {
		f.H.peers.mu.Lock()
		defer f.H.peers.mu.Unlock()
		sent = nil
		for _, message := range f.H.peers.sent {
			if message.ChatID == 12005 {
				sent = append(sent, message)
			}
		}
		return len(sent) > 0 && sent[len(sent)-1].ReplyMarkup != nil
	}, 5*time.Second, 10*time.Millisecond)
	var combined strings.Builder
	for i, message := range sent {
		combined.WriteString(message.Text)
		if i < len(sent)-1 {
			require.Nil(t, message.ReplyMarkup)
		}
		for _, entity := range message.Entities {
			require.NotEqual(t, "text_mention", entity.Type)
		}
	}
	text := combined.String()
	require.Contains(t, text, "Alex ``` **literal**")
	require.Contains(t, text, "Sam Follower")
	require.Contains(t, text, "Telegram 12002")
	require.Contains(t, text, "Telegram 12003")
	require.Contains(t, text, prep.Description)
	require.Contains(t, text, "deposit minimum 30.00 BYN")
	require.Contains(t, text, "Minimum amount: 30.00 BYN")
	require.Contains(t, text, "Minimum share: 25.00%")
	require.Contains(t, text, "tier 1 (early)")
	require.Contains(t, text, "Final top-up deadline:")
	require.Contains(t, text, "Confirm before:")
	require.Contains(t, text, prep.Timezone)
	require.NotContains(t, text, "INCOMPLETE UNTRUSTED SUMMARY")
	require.NotContains(t, text, "WRONG PERSON")
	require.NotContains(t, text, "expected_leader_version")
	view, err := f.H.store.PairAssignmentProposal(t.Context(), scoped, prep.ID)
	require.NoError(t, err)
	require.Equal(t, prep, view.Preparation)
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes, "Displaying approval must not assign passes")
}

func TestPairAssignmentApprovalDoesNotFallbackWhenArgumentsDoNotMatchPreparation(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), f.admin, "pair-mismatched-approval", f.args)
	require.NoError(t, err)
	changed := f.args
	changed.ExpectedFollowerVersion++
	raw, err := json.Marshal(changed)
	require.NoError(t, err)
	f.H.peers.mu.Lock()
	f.H.peers.approval = &loom.Approval{
		ID:          "pair-mismatched-approval",
		OperationID: "pair-mismatched-approval",
		Method:      "zinusya_pairs.assign",
		ReplyMode:   loom.ReplyModeAgent,
		Text:        "UNVERIFIED PAIR PROMPT",
		Arguments:   raw,
		ExpiresAt:   prep.ExpiresAt,
	}
	f.H.peers.mu.Unlock()
	_ = acceptPairApprovalRequest(t, f, 12005)
	require.Eventually(t, func() bool {
		f.H.peers.mu.Lock()
		defer f.H.peers.mu.Unlock()
		return len(f.H.peers.createAttempts)+f.H.peers.runGets >= 2
	}, 5*time.Second, 10*time.Millisecond)
	f.H.peers.mu.Lock()
	for _, message := range f.H.peers.sent {
		require.NotEqual(t, int64(12005), message.ChatID, "An unverifiable prompt must not offer consent")
	}
	f.H.peers.mu.Unlock()
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestPairAssignmentApprovalRequiresCurrentEventAuthorityBeforeDisplay(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	authority, err := f.H.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"grant-display-admin",
		db.AuthorityChange{EventID: f.EventID, UserID: f.Other.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), f.Other, "revoked-display-admin", f.args)
	require.NoError(t, err)
	_, err = f.H.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"revoke-display-admin",
		db.AuthorityChange{
			EventID:         f.EventID,
			UserID:          f.Other.UserID,
			Role:            db.AuthorityNone,
			ExpectedVersion: authority.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	raw, err := json.Marshal(f.args)
	require.NoError(t, err)
	f.H.peers.mu.Lock()
	f.H.peers.adminRole = false
	f.H.peers.approval = &loom.Approval{
		ID:          "revoked-display-admin",
		OperationID: "revoked-display-admin",
		Method:      "zinusya_pairs.assign",
		ReplyMode:   loom.ReplyModeAgent,
		Text:        "NO LONGER AUTHORIZED",
		Arguments:   raw,
		ExpiresAt:   prep.ExpiresAt,
	}
	f.H.peers.mu.Unlock()
	_ = acceptPairApprovalRequest(t, f, 12004)
	require.Eventually(t, func() bool {
		f.H.peers.mu.Lock()
		defer f.H.peers.mu.Unlock()
		return len(f.H.peers.createAttempts)+f.H.peers.runGets >= 2
	}, 5*time.Second, 10*time.Millisecond)
	f.H.peers.mu.Lock()
	for _, message := range f.H.peers.sent {
		require.NotEqual(
			t,
			int64(12004),
			message.ChatID,
			"Lost event authority must hide participant identities and consent",
		)
	}
	f.H.peers.mu.Unlock()
}

func TestPairAssignmentApprovalCanPresentOriginalReceiptAfterNativeCompletion(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), f.admin, "agent-before-native-completion", f.args)
	require.NoError(t, err)
	result, err := f.H.store.ConfirmPairAssignment(
		t.Context(),
		f.admin,
		"agent-before-native-completion",
		f.args,
		prep.ID,
	)
	require.NoError(t, err)
	raw, err := json.Marshal(f.args)
	require.NoError(t, err)
	f.H.peers.mu.Lock()
	f.H.peers.approval = &loom.Approval{
		ID:          "agent-before-native-completion",
		OperationID: "agent-before-native-completion",
		Method:      "zinusya_pairs.assign",
		ReplyMode:   loom.ReplyModeAgent,
		Text:        "OUTDATED PENDING SUMMARY",
		Arguments:   raw,
		ExpiresAt:   prep.ExpiresAt,
	}
	f.H.peers.mu.Unlock()
	id := acceptPairApprovalRequest(t, f, 12005)
	f.H.await(t, id, "awaiting_user")
	var text string
	require.Eventually(t, func() bool {
		f.H.peers.mu.Lock()
		defer f.H.peers.mu.Unlock()
		var combined strings.Builder
		ready := false
		for _, message := range f.H.peers.sent {
			if message.ChatID == 12005 {
				combined.WriteString(message.Text)
				ready = message.ReplyMarkup != nil
			}
		}
		text = combined.String()
		return ready
	}, 5*time.Second, 10*time.Millisecond)
	require.Contains(t, text, "This assignment was already completed")
	require.Contains(t, text, "reconcile the original result")
	require.Contains(t, text, "Alex Leader")
	require.Contains(t, text, "Telegram 12003")
	require.Contains(t, text, prep.Description)
	require.NotContains(t, text, "OUTDATED PENDING SUMMARY")
	replay, err := f.H.store.ConfirmPairAssignment(
		t.Context(),
		f.admin,
		"agent-before-native-completion",
		f.args,
		prep.ID,
	)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Equal(t, 2, passes)
}
