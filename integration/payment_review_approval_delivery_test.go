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
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func paymentApprovalFixture(
	t *testing.T,
	language string,
) (paymentFixture, db.PaymentScope, db.PaymentReviewCommitPreparation) {
	t.Helper()
	f, scope, review := reviewWorkspaceFixture(t)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_users SET profile=jsonb_build_object('language_code',$1::text) WHERE "+
			"id=$2", language, f.AmbassadorID)
	require.NoError(t, err)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		t.Context(),
		scope,
		"approval-review",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	prep, err := f.H.store.PreparePaymentReviewCommit(t.Context(), scope, "payment-approval", workspaceCommit(w))
	require.NoError(t, err)
	return f, scope, prep
}

func deliverPaymentApproval(
	t *testing.T,
	f paymentFixture,
	prep db.PaymentReviewCommitPreparation,
	language string,
) uuid.UUID {
	t.Helper()
	raw, err := json.Marshal(prep.Arguments)
	require.NoError(t, err)
	f.H.peers.mu.Lock()
	f.H.peers.approval = &loom.Approval{
		ID: "payment-approval", OperationID: "payment-approval",
		Method: "zinusya_payments.review_commit", ReplyMode: loom.ReplyModeAgent,
		Text: "INCOMPLETE UNTRUSTED SUMMARY", ConfirmationText: "WRONG AMOUNT",
		Arguments: raw, ArgumentHash: "bound-payment-action", ExpiresAt: prep.ExpiresAt,
	}
	f.H.peers.mu.Unlock()
	f.H.start(t)
	update := telegram.Update{ID: 99501, Message: &telegram.Message{
		ID: 99501, From: &telegram.User{ID: 11003, LanguageCode: language},
		Chat: telegram.Chat{ID: 11003, Type: "private"}, Text: "Show the prepared payment decision",
	}}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := f.H.service.Accept(t.Context(), update, body)
	require.NoError(t, err)
	return accepted.ID
}

func TestPaymentReviewApprovalDisplaysStoredTermsWithoutTechnicalArguments(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, scope, prep := paymentApprovalFixture(t, language)
			id := deliverPaymentApproval(t, f, prep, language)
			f.H.await(t, id, "awaiting_user")
			text := paymentConsentText(t, f)
			require.NotContains(t, text, "INCOMPLETE UNTRUSTED SUMMARY")
			require.NotContains(t, text, "WRONG AMOUNT")
			require.NotContains(t, text, `"workspace_id"`)
			require.NotContains(t, text, `"expected_version"`)
			require.Contains(t, text, "30.87 BYN")
			require.Contains(t, text, "Europe/Minsk")
			if language == "ru" {
				require.Contains(t, text, "Остаток после этого решения:")
			} else {
				require.Contains(t, text, "Remaining after this decision:")
			}
			// Markdown fences are Telegram entities; all original human terms remain literal.
			for line := range strings.SplitSeq(prep.Description, "\n") {
				if !strings.HasPrefix(line, "```") {
					require.Contains(t, text, line)
				}
			}
			receipt, err := f.H.store.PaymentReviewWorkspaceOperation(t.Context(), scope, "payment-approval")
			require.NoError(t, err)
			require.False(t, receipt.Completed)
			require.Equal(t, prep, *receipt.Preparation)
			balance, err := f.H.store.PaymentBalance(
				t.Context(),
				paymentScope(t, f.H, f.OwnerID, false),
				f.Event.ID,
				f.Pass.ID,
			)
			require.NoError(t, err)
			require.Zero(t, balance.ConfirmedMinor, "Showing consent must not credit money")
		})
	}
}

func TestPaymentReviewApprovalDoesNotOfferUnverifiableConsent(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"amount", "expiry", "authority"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			f, scope, prep := paymentApprovalFixture(t, "en")
			switch invalid {
			case "amount":
				prep.Arguments.Review.AmountMinor++
			case "expiry":
				prep.ExpiresAt = prep.ExpiresAt.Add(time.Hour)
			case "authority":
				role, err := f.H.store.Authority(t.Context(), scope.EventScope, f.Event.ID, scope.UserID)
				require.NoError(t, err)
				_, err = f.H.store.ChangeAuthority(
					t.Context(),
					scope.EventScope,
					paymentScope(t, f.H, f.AdminID, true).Subject,
					"revoke-reviewer",
					db.AuthorityChange{
						EventID:         f.Event.ID,
						UserID:          scope.UserID,
						Role:            db.AuthorityNone,
						ExpectedVersion: role.Version,
					},
					uuid.Nil,
				)
				require.NoError(t, err)
			}
			_ = deliverPaymentApproval(t, f, prep, "en")
			// Observe repeated polling, then drain the worker before checking delivery.
			require.Eventually(t, func() bool {
				f.H.peers.mu.Lock()
				defer f.H.peers.mu.Unlock()
				return len(f.H.peers.createAttempts)+f.H.peers.runGets >= 2
			}, 5*time.Second, 10*time.Millisecond)
			f.H.stop(t)
			f.H.peers.mu.Lock()
			sent := append([]telegram.SendMessageRequest(nil), f.H.peers.sent...)
			f.H.peers.mu.Unlock()
			title, _, _ := strings.Cut(prep.Description, "\n")
			for _, m := range sent {
				if m.ChatID != 11003 {
					continue
				}
				// A pending-proof notice is allowed; an unverified approval is not.
				require.NotContains(t, m.Text, title)
				require.NotContains(t, m.Text, "INCOMPLETE UNTRUSTED SUMMARY")
				require.NotContains(t, m.Text, "WRONG AMOUNT")
				_, approvalKeyboard := m.ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
				require.False(t, approvalKeyboard, "Unverified consent must not offer approval choices")
			}
			balance, err := f.H.store.PaymentBalance(
				t.Context(),
				paymentScope(t, f.H, f.OwnerID, false),
				f.Event.ID,
				f.Pass.ID,
			)
			require.NoError(t, err)
			require.Zero(t, balance.ConfirmedMinor)
		})
	}
}

func TestPaymentReviewApprovalCanReconcileOriginalDecisionAfterCompletion(t *testing.T) {
	t.Parallel()
	for _, completion := range []string{"native", "agent"} {
		t.Run(completion, func(t *testing.T) {
			t.Parallel()
			f, scope, prep := paymentApprovalFixture(t, "en")
			var original db.PaymentWorkspaceReviewResult
			var err error
			if completion == "native" {
				session := nativePaymentSession(t, f, scope, 11003)
				w, readErr := f.H.store.CurrentPaymentReviewWorkspace(
					t.Context(),
					scope,
					prep.Arguments.Review.EventID,
					prep.Arguments.Review.EvidenceID,
				)
				require.NoError(t, readErr)
				original, err = f.H.store.CommitMiniAppPaymentReviewWorkspace(
					t.Context(),
					scope,
					session,
					"native-confirm",
					nativeReviewRequest(w),
				)
			} else {
				original, err = f.H.store.CommitPaymentReviewWorkspace(
					t.Context(),
					scope,
					"payment-approval",
					prep.Arguments,
					prep.ID,
				)
			}
			require.NoError(t, err)
			id := deliverPaymentApproval(t, f, prep, "en")
			f.H.await(t, id, "awaiting_user")
			text := paymentConsentText(t, f)
			require.Contains(t, text, "already completed")
			require.Contains(t, text, "30.87 BYN")
			replay, err := f.H.store.CommitPaymentReviewWorkspace(
				t.Context(),
				scope,
				"payment-approval",
				prep.Arguments,
				prep.ID,
			)
			require.NoError(t, err)
			require.Equal(t, original, replay)
			balance, err := f.H.store.PaymentBalance(
				t.Context(),
				paymentScope(t, f.H, f.OwnerID, false),
				f.Event.ID,
				f.Pass.ID,
			)
			require.NoError(t, err)
			require.EqualValues(t, 3087, balance.ConfirmedMinor)
		})
	}
}

// Wait for the final keyboard before reading the whole ordered consent.
func paymentConsentText(t *testing.T, f paymentFixture) string {
	t.Helper()
	var sent []telegram.SendMessageRequest
	require.Eventually(t, func() bool {
		f.H.peers.mu.Lock()
		defer f.H.peers.mu.Unlock()
		sent = nil
		for _, m := range f.H.peers.sent {
			if m.ChatID == 11003 {
				sent = append(sent, m)
			}
		}
		return len(sent) > 0 && sent[len(sent)-1].ReplyMarkup != nil
	}, 5*time.Second, 10*time.Millisecond)
	var text strings.Builder
	for n, m := range sent {
		text.WriteString(m.Text)
		if n < len(sent)-1 {
			require.Nil(t, m.ReplyMarkup)
		}
	}
	return text.String()
}
