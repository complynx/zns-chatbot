package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ReplyMarkup limits sendMessage keyboards to the supported Telegram variants.
type ReplyMarkup interface {
	replyMarkup()
}

type KeyboardButton struct {
	Text string `json:"text"`
}

type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

type ForceReply struct {
	ForceReply            bool   `json:"force_reply"`
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
}

func (*ForceReply) replyMarkup() {}

type ReplyKeyboardRemove struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}

func (*InlineKeyboardMarkup) replyMarkup() {}
func (*ReplyKeyboardMarkup) replyMarkup()  {}
func (*ReplyKeyboardRemove) replyMarkup()  {}

// UnmarshalJSON restores the concrete keyboard when replaying a saved outbox row.
func (r *SendMessageRequest) UnmarshalJSON(data []byte) error {
	type plain SendMessageRequest
	*r = SendMessageRequest{}
	value := struct {
		*plain

		ReplyMarkup json.RawMessage `json:"reply_markup"`
	}{plain: (*plain)(r)}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if len(value.ReplyMarkup) == 0 || bytes.Equal(bytes.TrimSpace(value.ReplyMarkup), []byte("null")) {
		return nil
	}
	var shape struct {
		Inline json.RawMessage `json:"inline_keyboard"`
		Reply  json.RawMessage `json:"keyboard"`
		Remove json.RawMessage `json:"remove_keyboard"`
		Force  json.RawMessage `json:"force_reply"`
	}
	if err := json.Unmarshal(value.ReplyMarkup, &shape); err != nil {
		return err
	}
	variants := 0
	if shape.Inline != nil {
		r.ReplyMarkup = &InlineKeyboardMarkup{}
		variants++
	}
	if shape.Reply != nil {
		r.ReplyMarkup = &ReplyKeyboardMarkup{}
		variants++
	}
	if shape.Remove != nil {
		r.ReplyMarkup = &ReplyKeyboardRemove{}
		variants++
	}
	if shape.Force != nil {
		r.ReplyMarkup = &ForceReply{}
		variants++
	}
	if variants != 1 {
		return errors.New("invalid Telegram reply markup")
	}
	return json.Unmarshal(value.ReplyMarkup, r.ReplyMarkup)
}
