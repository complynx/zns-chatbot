package telegram

import (
	"context"
	"slices"
)

// ReactionEmoji is the ordinary-emoji repertoire offered to the agent. Return a copy.
func ReactionEmoji() []string {
	return []string{"❤", "👍", "🥰", "👏", "😁", "🤔", "🤯", "😱", "😢", "🎉",
		"🤩", "🙏", "👌", "😍", "💯", "🤣", "💔", "🤨", "🙈", "😇", "🤝", "🤗", "🫡"}
}

func SupportedReaction(value string) bool {
	return value == "" || slices.Contains(ReactionEmoji(), value)
}

type ReactionType struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

// SetMessageReaction replaces the bot's selection; an empty value removes it.
// Telegram bots can select at most one ordinary reaction per message.
func (c *Client) SetMessageReaction(ctx context.Context, chatID, messageID int64, emoji string) error {
	if chatID == 0 || messageID <= 0 || !SupportedReaction(emoji) {
		return &Error{Outcome: NotStarted}
	}
	reactions := []ReactionType{}
	if emoji != "" {
		reactions = append(reactions, ReactionType{Type: "emoji", Emoji: emoji})
	}
	var result bool
	err := c.call(ctx, "setMessageReaction", struct {
		ChatID    int64          `json:"chat_id"`
		MessageID int64          `json:"message_id"`
		Reactions []ReactionType `json:"reaction"`
	}{chatID, messageID, reactions}, &result, true)
	if err == nil && !result {
		return &Error{Outcome: Unknown}
	}
	return err
}
