package task

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

var ErrUnsupportedUpdate = errors.New("unsupported Telegram update")

type intent struct {
	NativeAvatar         bool             `json:"native_avatar,omitempty"`
	RegistrationPromptID uuid.UUID        `json:"registration_prompt_id,omitempty"`
	RegistrationRole     string           `json:"registration_role,omitempty"`
	CallbackMessageID    int64            `json:"callback_message_id,omitempty"`
	ReplyToMessageID     int64            `json:"reply_to_message_id,omitempty"`
	StructuredCommand    string           `json:"structured_command,omitempty"`
	PartnerCode          *uuid.UUID       `json:"partner_code,omitempty"`
	Attachments          []files.Incoming `json:"attachments,omitempty"`
	PreferredLanguage    string           `json:"preferred_language,omitempty"`
	TraceParent          string           `json:"traceparent,omitempty"`
	TraceState           string           `json:"tracestate,omitempty"`
	Kind                 string           `json:"kind"`
	User                 telegram.User    `json:"user"`
	ChatID               int64            `json:"chat_id"`
	Text                 string           `json:"text,omitempty"`
	RawUpdate            json.RawMessage  `json:"raw_update"`
	CallbackTaskID       uuid.UUID        `json:"callback_task_id,omitempty"`
	PromptKey            string           `json:"prompt_key,omitempty"`
	Decision             string           `json:"decision,omitempty"`
}

func normalize(u telegram.Update, raw json.RawMessage) (intent, error) {
	i := intent{Kind: kindMessage, RawUpdate: raw}
	if u.ID < 0 || !json.Valid(raw) {
		return i, ErrUnsupportedUpdate
	}
	var err error
	switch {
	case u.Message != nil && u.CallbackQuery == nil:
		i, err = messageIntent(i, u.Message)
	case u.CallbackQuery != nil && u.Message == nil:
		i, err = callbackIntent(i, u.CallbackQuery)
	default:
		return i, ErrUnsupportedUpdate
	}
	if err != nil {
		return i, err
	}
	if i.User.ID <= 0 || i.User.ID > 1<<52-1 || i.User.IsBot || i.ChatID != i.User.ID {
		return i, ErrUnsupportedUpdate
	}
	return i, nil
}
func messageIntent(i intent, m *telegram.Message) (intent, error) {
	if m.From == nil || m.Chat.Type != "private" {
		return i, ErrUnsupportedUpdate
	}
	i.User = *m.From
	i.ChatID = m.Chat.ID
	i.Text = m.Text
	var err error
	i.Attachments, err = files.FromMessage(m)
	if err != nil {
		return i, ErrUnsupportedUpdate
	}
	if len(i.Attachments) > 0 {
		i.Text = m.Caption
	}
	if len(i.Attachments) == 0 {
		i.PartnerCode = telegram.PartnerStartCode(m.Text)
	}
	if (strings.TrimSpace(i.Text) == "" && len(i.Attachments) == 0) || unsupportedMedia(i.RawUpdate) {
		i.Kind = kindUnsupported
		i.Attachments = nil
	}
	if i.Kind == kindMessage && m.ReplyToMessage != nil {
		i.ReplyToMessageID = m.ReplyToMessage.ID
	}
	i.NativeAvatar = i.Kind == kindMessage && nativeAvatarImage(i.Attachments) &&
		strings.TrimSpace(i.Text) == "" && i.ReplyToMessageID == 0
	if i.Kind == kindMessage && len(i.Attachments) == 0 && structuredCommand(m.Text) {
		i.Kind = kindStructured
		i.StructuredCommand = strings.Fields(strings.TrimSpace(m.Text))[0]
	}
	return i, nil
}

func nativeAvatarImage(attachments []files.Incoming) bool {
	if len(attachments) != 1 {
		return false
	}
	a := attachments[0]
	return a.Kind == "photo" || a.Kind == "document" && strings.HasPrefix(a.MIMEType, "image/")
}

func unsupportedMedia(raw json.RawMessage) bool {
	var update map[string]json.RawMessage
	var message map[string]json.RawMessage
	_ = json.Unmarshal(raw, &update)
	_ = json.Unmarshal(update["message"], &message)
	for _, key := range []string{"sticker", "contact", "location", "poll", "dice", "paid_media", "live_photo",
		"rich_message"} {
		if _, ok := message[key]; ok {
			return true
		}
	}
	return false
}
func callbackIntent(i intent, cb *telegram.CallbackQuery) (intent, error) {
	if cb.Message == nil || cb.Message.Chat.Type != "private" {
		return i, ErrUnsupportedUpdate
	}
	i.User = cb.From
	i.ChatID = cb.Message.Chat.ID
	if normalized, matched, err := registrationCallback(i, cb); matched {
		return normalized, err
	}
	if cursor, ok := strings.CutPrefix(cb.Data, registrationPageCallback); ok {
		after, err := uuid.Parse(cursor)
		if err != nil || after == uuid.Nil || after.String() != cursor {
			return i, ErrUnsupportedUpdate
		}
		i.Kind, i.StructuredCommand = kindStructured, registrationsCommand
		i.Text = registrationsCommand + " " + cursor
		return i, nil
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 4 || len(cb.Data) > 64 {
		return i, ErrUnsupportedUpdate
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil || len(parts[2]) != 8 {
		return i, ErrUnsupportedUpdate
	}
	if _, err = hex.DecodeString(parts[2]); err != nil {
		return i, ErrUnsupportedUpdate
	}
	i.CallbackTaskID = id
	i.PromptKey = parts[2]
	return callbackChoice(i, parts[0], parts[3])
}
func callbackChoice(i intent, kind, value string) (intent, error) {
	switch kind {
	case "a":
		i.Kind = kindApproval
		i.Decision = value
		if value != "approve" && value != "reject" {
			return i, ErrUnsupportedUpdate
		}
	default:
		return i, ErrUnsupportedUpdate
	}
	return i, nil
}
func promptKey(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:4])
}
