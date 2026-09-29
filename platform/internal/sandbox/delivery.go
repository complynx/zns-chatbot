package sandbox

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const channelID int64 = -1009001
const forumID int64 = -1009002

// These destinations are synthetic fixtures, never external Telegram identities.
func destination(value string) (telegram.Chat, bool) {
	switch strings.ToLower(value) {
	case "@sandbox_channel", "-1009001":
		return telegram.Chat{ID: channelID, Type: "channel", Username: "sandbox_channel"}, true
	case "@sandbox_forum", "-1009002":
		return telegram.Chat{ID: forumID, Type: "supergroup", Username: "sandbox_forum"}, true
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return telegram.Chat{}, false
	}
	_, ok := identity.Subject(id)
	return telegram.Chat{ID: id, Type: privateChat}, ok
}

type deliveryRequest struct {
	ChatID    json.RawMessage `json:"chat_id"`
	ThreadID  int64           `json:"message_thread_id,omitempty"`
	MessageID int64           `json:"message_id,omitempty"`
	Text      string          `json:"text"`
	ParseMode string          `json:"parse_mode,omitempty"`
	Markup    telegram.Markup `json:"reply_markup"`
}

func (p deliveryRequest) resolve() (telegram.Send, telegram.Chat, error) {
	value := string(p.ChatID)
	if strings.HasPrefix(value, "\"") {
		if err := json.Unmarshal(p.ChatID, &value); err != nil {
			return telegram.Send{}, telegram.Chat{}, err
		}
	}
	chat, ok := destination(value)
	if !ok {
		return telegram.Send{}, chat, errors.New("chat not found")
	}
	if p.ThreadID != 0 && (chat.ID != forumID || p.ThreadID != 101 && p.ThreadID != 102) {
		return telegram.Send{}, chat, errors.New("message thread not found")
	}
	return telegram.Send{
		ChatID:    chat.ID,
		MessageID: p.MessageID,
		Text:      p.Text,
		ParseMode: p.ParseMode,
		Markup:    p.Markup,
	}, chat, nil
}
