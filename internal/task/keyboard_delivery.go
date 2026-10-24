package task

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// Fixed stripes bound memory; only chat-wide keyboard changes share this gate.
const keyboardLockCount = 64

func (s *Service) sendKeyboardText(ctx context.Context, row db.Delivery) (telegram.Receipt, string, error) {
	var payload telegram.SendMessageRequest
	if json.Unmarshal(row.Payload, &payload) != nil {
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.Rejected}
	}
	switch payload.ReplyMarkup.(type) {
	case *telegram.ReplyKeyboardMarkup, *telegram.ReplyKeyboardRemove:
	default:
		receipt, err := s.sendText(ctx, row.Payload)
		return receipt, "", err
	}
	state, err := s.store.KeyboardState(ctx, row.TaskID)
	if err != nil {
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.NotStarted, Retryable: true}
	}
	var current *loom.Run
	if json.Unmarshal(state.Run, &current) != nil {
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.NotStarted, Retryable: true}
	}
	pending := liveKeyboardID(current)
	switch payload.ReplyMarkup.(type) {
	case *telegram.ReplyKeyboardRemove:
		if state.KeyboardOwnerTaskID != uuid.Nil && state.KeyboardOwnerTaskID != row.TaskID || pending != "" {
			payload.ReplyMarkup = nil
		}
	case *telegram.ReplyKeyboardMarkup:
		prefix := row.TaskID.String() + ":"
		agent := strings.HasPrefix(row.DedupKey, prefix+"question:dialog-v1:") ||
			strings.HasPrefix(row.DedupKey, prefix+"approval:dialog-v1:")
		if agent {
			if state.ActiveTaskID != row.TaskID || pending == "" ||
				!strings.HasPrefix(row.DedupKey, prefix+pending+":") {
				payload.ReplyMarkup = nil
			}
		} else if pending != "" {
			// Menu reads leave the unresolved agent choices available in the chat.
			payload.ReplyMarkup = nil
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return telegram.Receipt{}, "", &telegram.Error{Outcome: telegram.Rejected}
	}
	receipt, err := s.sendText(ctx, raw)
	// Record actual wire markup, never claim suppressed choices were presented.
	return receipt, replyMarkupHash(payload.ReplyMarkup), err
}

// Hold through FinishDelivery so the next sender sees the actual prior wire order.
func (s *Service) lockKeyboardDelivery(ctx context.Context, row db.Delivery) (func(), error) {
	var payload telegram.SendMessageRequest
	if json.Unmarshal(row.Payload, &payload) != nil {
		return func() {}, &telegram.Error{Outcome: telegram.Rejected}
	}
	switch payload.ReplyMarkup.(type) {
	case *telegram.ReplyKeyboardMarkup, *telegram.ReplyKeyboardRemove:
	default:
		return func() {}, nil
	}
	task, err := s.store.GetTask(ctx, row.TaskID)
	if err != nil {
		return func() {}, &telegram.Error{Outcome: telegram.NotStarted, Retryable: true}
	}
	gate := &s.keyboardSend[int(task.ConversationID[0])%keyboardLockCount]
	gate.Lock()
	return gate.Unlock, nil
}
