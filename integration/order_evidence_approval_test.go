package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestOrderEvidenceApprovalDeliversFullStoredSummaryBeforeKeyboard(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.fileBytes = []byte("%PDF-1.4\nfixture document")
	f, owner, args := orderApprovalFixture(t, p, true)
	orderOp := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), owner, orderOp, args, db.OrderSourceAgent)
	require.NoError(t, err)
	order, err := f.H.store.SubmitOwnOrder(t.Context(), owner, orderOp, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	fileUpdate := telegram.Update{
		ID: 61511,
		Message: &telegram.Message{
			ID:      61511,
			Date:    time.Now().UTC().Unix(),
			From:    &telegram.User{ID: 11002},
			Chat:    telegram.Chat{ID: 11002, Type: "private"},
			Caption: "Receive this synthetic proof only",
			Document: &telegram.MediaFile{
				FileID:   "private-telegram-file",
				UniqueID: "full-order-review-summary",
				MIMEType: "application/pdf",
				Filename: "proof.pdf",
			},
		},
	}
	filePayload, err := json.Marshal(fileUpdate)
	require.NoError(t, err)
	fileTask, _, err := f.H.service.Accept(t.Context(), fileUpdate, filePayload)
	require.NoError(t, err)
	f.H.await(t, fileTask.ID, "completed")
	stored, err := f.H.store.GetTask(t.Context(), fileTask.ID)
	require.NoError(t, err)
	var fileIntent struct {
		Attachments []struct {
			ID uuid.UUID `json:"id"`
		} `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(stored.Request, &fileIntent))
	require.Len(t, fileIntent.Attachments, 1)
	file := fileIntent.Attachments[0].ID
	f.H.stop(t)
	evidence, err := f.H.store.SubmitOwnOrderEvidence(
		t.Context(),
		owner,
		uuid.New(),
		db.OrderEvidenceSubmission{
			EventID:              f.Event.ID,
			OrderID:              order.Order.ID,
			ExpectedOrderVersion: order.Order.Version,
			FileID:               file,
			AmountMinor:          order.Order.Snapshot.Quote.TotalMinor,
		},
	)
	require.NoError(t, err)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{
			Language: "ru",
			Review: db.OrderEvidenceReview{
				EventID:         f.Event.ID,
				EvidenceID:      evidence.ID,
				ExpectedVersion: 1,
				Decision:        db.OrderEvidenceRejected,
				Reason:          "Literal ``` **reason** end-canary",
			},
		},
	)
	require.NoError(t, err)
	op := uuid.New()
	approval, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, orderReviewCommit(w))
	require.NoError(t, err)
	raw, err := json.Marshal(approval.Arguments)
	require.NoError(t, err)
	p.mu.Lock()
	p.sent = nil
	p.approval = &loom.Approval{
		ID:           op.String(),
		OperationID:  op.String(),
		Method:       "zinusya_order_payments.review_commit",
		ReplyMode:    loom.ReplyModeAgent,
		Text:         "ABBREVIATED PROMPT MUST NOT BE SENT",
		Arguments:    raw,
		ArgumentHash: "exact-review-hash",
		ExpiresAt:    approval.ExpiresAt,
	}
	p.mu.Unlock()
	f.H.start(t)
	update := telegram.Update{
		ID: 61512,
		Message: &telegram.Message{
			ID:   61512,
			From: &telegram.User{ID: 11003},
			Chat: telegram.Chat{ID: 11003, Type: "private"},
			Text: "Отклони проверенное подтверждение оплаты",
		},
	}
	payload, err := json.Marshal(update)
	require.NoError(t, err)
	task, _, err := f.H.service.Accept(t.Context(), update, payload)
	require.NoError(t, err)
	f.H.await(t, task.ID, "awaiting_user")
	var sent []telegram.SendMessageRequest
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		sent = append([]telegram.SendMessageRequest(nil), p.sent...)
		return len(sent) > 1 && sent[len(sent)-1].ReplyMarkup != nil
	}, 5*time.Second, 10*time.Millisecond)
	var shown strings.Builder
	for n, message := range sent {
		require.LessOrEqual(t, len(utf16.Encode([]rune(message.Text))), 4096)
		shown.WriteString(message.Text)
		if n < len(sent)-1 {
			require.Nil(t, message.ReplyMarkup)
		}
		for _, entity := range message.Entities {
			require.NotEqual(t, "text_mention", entity.Type)
		}
	}
	text := shown.String()
	require.NotContains(t, text, "ABBREVIATED PROMPT MUST NOT BE SENT")
	require.Contains(t, text, "Отклонить подтверждение оплаты заказа?")
	require.Contains(t, text, "Подтвердить сумму: 0.00 BYN")
	require.Contains(t, text, "988.20 BYN")
	require.Contains(t, text, "end-canary")
	require.Contains(t, text, evidence.ID.String())
	require.Contains(t, text, order.Order.ID.String())
	require.Equal(t, 40, strings.Count(text, "24.68 BYN"), "All forty item lines must reach Telegram")
	current, err := f.H.store.OrderEvidenceForReview(t.Context(), scope, f.Event.ID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, evidence.State, current.State, "Showing consent must not decide evidence")
}
