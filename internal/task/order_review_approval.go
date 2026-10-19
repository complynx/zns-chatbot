package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

// Deliver the whole stored consent before the last-chunk reply keyboard, even
// when Loom's human prompt was abbreviated. Arguments/expiry must match exactly.
func (s *Service) orderReviewApprovalText(ctx context.Context, t db.Task, i intent, a loom.Approval) (string, error) {
	op, err := uuid.Parse(a.OperationID)
	if err != nil || op == uuid.Nil {
		return "", errors.New("invalid order review operation")
	}
	var args db.OrderReviewCommitChange
	decoder := json.NewDecoder(bytes.NewReader(a.Arguments))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&args); err != nil {
		return "", err
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("trailing order review arguments")
	}
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return "", err
	}
	if user.BotID != s.options.BotID || user.TelegramUserID != i.User.ID {
		return "", errors.New("order review recipient mismatch")
	}
	principal, err := s.taskPrincipal(ctx, user)
	if err != nil {
		return "", err
	}
	receipt, err := s.store.OrderReviewOperationByID(ctx, s.taskPaymentScope(user, principal), op)
	if err != nil {
		return "", err
	}
	p := receipt.Preparation
	if receipt.Source != "agent" || receipt.Kind != "commit" || p == nil || p.Description == "" ||
		p.Arguments != args ||
		a.ExpiresAt.After(p.ExpiresAt) {
		return "", errors.New("order review approval does not match stored consent")
	}
	return p.Description, nil
}
