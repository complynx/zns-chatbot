package task

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/shuttle/internal/telegram"
)

var ErrUnsupportedUpdate = errors.New("unsupported Telegram update")

type intent struct {
	TraceParent    string          `json:"traceparent,omitempty"`
	TraceState     string          `json:"tracestate,omitempty"`
	Kind           string          `json:"kind"`
	User           telegram.User   `json:"user"`
	ChatID         int64           `json:"chat_id"`
	Text           string          `json:"text,omitempty"`
	RawUpdate      json.RawMessage `json:"raw_update"`
	CallbackTaskID uuid.UUID       `json:"callback_task_id,omitempty"`
	PromptKey      string          `json:"prompt_key,omitempty"`
	ChoiceIndex    int             `json:"choice_index,omitempty"`
	Decision       string          `json:"decision,omitempty"`
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
	if strings.TrimSpace(m.Text) == "" || unsupportedMedia(i.RawUpdate) {
		i.Kind = kindUnsupported
	}
	if strings.TrimSpace(m.Text) == "/cancel" {
		i.Kind = "cancel"
	}
	return i, nil
}
func unsupportedMedia(raw json.RawMessage) bool {
	var update map[string]json.RawMessage
	var message map[string]json.RawMessage
	_ = json.Unmarshal(raw, &update)
	_ = json.Unmarshal(update["message"], &message)
	for _, key := range []string{"sticker", "photo", "video", "document", "audio", "voice", "video_note", "animation", "contact", "location", "poll", "dice"} {
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
	case "q":
		i.Kind = "input"
		index, err := strconv.Atoi(value)
		if err != nil || index < 0 || index >= maxChoices {
			return i, ErrUnsupportedUpdate
		}
		i.ChoiceIndex = index
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
