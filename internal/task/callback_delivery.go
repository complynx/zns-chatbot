package task

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	editCallbackTextKind   = "edit_callback_text"
	editCallbackMarkupKind = "edit_callback_markup"
)

type callbackMessageEditor interface {
	EditMessageText(context.Context, telegram.EditMessageRequest) (telegram.Receipt, error)
	EditMessageMarkup(context.Context, telegram.EditMessageMarkupRequest) (telegram.Receipt, error)
}

// Preserve the outbound text and keyboard fields used to validate later callbacks.
func editCallbackOutput(i intent, out []db.Outbound) []db.Outbound {
	return editCallbackMessage(i, out, editCallbackTextKind)
}

func approvalCallbackData(taskID uuid.UUID, prompt, choice string) string {
	return "a:" + taskID.String() + ":" + prompt + ":" + choice
}

func (s *Service) closeRunApprovalKeyboard(
	ctx context.Context, t db.Task, i intent, out []db.Outbound,
) ([]db.Outbound, error) {
	if i.CallbackMessageID <= 0 || i.CallbackTaskID == uuid.Nil || len(out) == 0 {
		return out, nil
	}
	callback := approvalCallbackData(i.CallbackTaskID, i.PromptKey, i.Decision)
	matched, err := s.store.HasDeliveredCallback(ctx, t, i.CallbackTaskID, i.ChatID, i.CallbackMessageID, callback)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "approval_callback_receipt", err)
		return out, err
	}
	if !matched {
		return out, nil
	}
	return editCallbackMessage(i, out, editCallbackMarkupKind), nil
}

func editCallbackMessage(i intent, out []db.Outbound, kind string) []db.Outbound {
	if i.CallbackMessageID <= 0 || i.ChatID != i.User.ID || len(out) == 0 {
		return out
	}
	subscription := i.StructuredCommand == communicationsCommand && !communicationDecision(i.Text)
	if kind == editCallbackTextKind && (subscription || i.Kind == kindApproval) {
		// Preserve human-authored broadcasts and the original terms of run approvals.
		return out
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(out[0].Payload, &input) != nil || input == nil {
		return out
	}
	input["kind"] = json.RawMessage(strconv.Quote(kind))
	input["edit_message_id"] = json.RawMessage(strconv.FormatInt(i.CallbackMessageID, 10))
	if raw, err := json.Marshal(input); err == nil {
		out[0].Payload = raw
	}
	return out
}

func (s *Service) sendCallbackEdit(
	ctx context.Context,
	row db.Delivery,
	kind string,
) (telegram.Receipt, string, error) {
	var delivery struct {
		MessageID int64 `json:"edit_message_id"`
	}
	var message telegram.SendMessageRequest
	if json.Unmarshal(row.Payload, &delivery) != nil || delivery.MessageID <= 0 ||
		json.Unmarshal(row.Payload, &message) != nil {
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.Rejected}
	}
	editor, ok := s.tg.(callbackMessageEditor)
	if !ok {
		return s.sendKeyboardText(ctx, row)
	}
	keyboard, _ := message.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	if keyboard == nil {
		keyboard = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{}}
	}
	var receipt telegram.Receipt
	var err error
	if kind == editCallbackMarkupKind {
		receipt, err = editor.EditMessageMarkup(ctx, telegram.EditMessageMarkupRequest{
			ChatID: message.ChatID, MessageID: delivery.MessageID, ReplyMarkup: keyboard,
		})
	} else {
		receipt, err = editor.EditMessageText(ctx, telegram.EditMessageRequest{
			ChatID: message.ChatID, MessageID: delivery.MessageID, Text: message.Text,
			Entities: message.Entities, ReplyMarkup: keyboard, LinkPreviewOptions: message.LinkPreviewOptions,
		})
	}
	if failure, rejected := errors.AsType[*telegram.Error](err); rejected &&
		failure.Outcome == telegram.Rejected && failure.ReasonClass() == "message_not_editable" {
		return s.sendKeyboardText(ctx, row)
	}
	return receipt, "", err
}
