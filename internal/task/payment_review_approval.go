package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

// Read the actor's original prepared decision so an abbreviated Loom prompt
// cannot replace its human terms. Displaying consent never confirms payment.
func (s *Service) paymentReviewApprovalText(ctx context.Context, t db.Task, i intent, a loom.Approval) (string, error) {
	var args db.PaymentReviewCommitChange
	decoder := json.NewDecoder(bytes.NewReader(a.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("trailing payment review arguments")
	}
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return "", err
	}
	if user.BotID != s.options.BotID || user.TelegramUserID != i.User.ID {
		return "", errors.New("payment review recipient mismatch")
	}
	principal, err := s.taskPrincipal(ctx, user)
	if err != nil {
		return "", err
	}
	receipt, err := s.store.PaymentReviewWorkspaceOperation(ctx, s.taskPaymentScope(user, principal), a.OperationID)
	if err != nil {
		return "", err
	}
	p := receipt.Preparation
	if receipt.Kind != "commit" || receipt.UnavailableReason != "" ||
		p == nil || p.Description == "" || p.Arguments != args || a.ExpiresAt.After(p.ExpiresAt) {
		return "", errors.New("payment review approval does not match stored consent")
	}
	if receipt.Completed {
		note := structuredText(
			s.languageFor(ctx, i) == "ru",
			"This payment decision was already completed. Continue to reconcile the original result; this is not a new payment.",
			"Это решение по оплате уже выполнено. Продолжи, чтобы получить исходный результат; это не новая оплата.",
		)
		return note + "\n\n" + p.Description, nil
	}
	return p.Description, nil
}
